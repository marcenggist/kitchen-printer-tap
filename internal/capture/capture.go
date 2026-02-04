package capture

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"github.com/marcenggist/kitchen-printer-tap/internal/config"
	"github.com/marcenggist/kitchen-printer-tap/internal/job"
)

// Stats holds capture statistics.
type Stats struct {
	JobsCaptured  atomic.Int64
	BytesCaptured atomic.Int64
	ParseErrors   atomic.Int64
}

// Capturer handles packet capture and job assembly.
type Capturer struct {
	cfg      *config.Config
	store    *job.Store
	reprint  *job.ReprintDetector
	stats    *Stats
	logger   *slog.Logger
	handle   *pcap.Handle
	sessions map[string]*session
	mu       sync.Mutex
	done     chan struct{}
	wg       sync.WaitGroup
}

// session tracks a TCP connection's data.
type session struct {
	job         *job.Job
	lastSeen    time.Time
	srcIP       string
	dstIP       string
	srcPort     uint16
	dstPort     uint16
	transport   string
	seqTracker  map[uint32]bool
	nextSeq     uint32
	initialized bool
}

// New creates a new packet capturer.
func New(cfg *config.Config, store *job.Store, reprint *job.ReprintDetector, stats *Stats, logger *slog.Logger) *Capturer {
	return &Capturer{
		cfg:      cfg,
		store:    store,
		reprint:  reprint,
		stats:    stats,
		logger:   logger,
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}
}

// Start begins packet capture.
func (c *Capturer) Start() error {
	filter := c.buildBPFFilter()

	handle, err := pcap.OpenLive(
		c.cfg.Interface,
		int32(c.cfg.Capture.SnapLen),
		c.cfg.Capture.Promiscuous,
		pcap.BlockForever,
	)
	if err != nil {
		return fmt.Errorf("opening interface %s: %w", c.cfg.Interface, err)
	}

	if err := handle.SetBPFFilter(filter); err != nil {
		handle.Close()
		return fmt.Errorf("setting BPF filter: %w", err)
	}

	c.handle = handle
	c.logger.Info("capture started",
		"interface", c.cfg.Interface,
		"filter", filter)

	// Start session timeout checker
	c.wg.Add(1)
	go c.sessionTimeoutLoop()

	// Start packet processing
	c.wg.Add(1)
	go c.processPackets()

	return nil
}

// Stop halts packet capture.
func (c *Capturer) Stop() {
	close(c.done)
	if c.handle != nil {
		c.handle.Close()
	}
	c.wg.Wait()

	// Close all remaining sessions
	c.mu.Lock()
	for key, sess := range c.sessions {
		c.finalizeSession(sess)
		delete(c.sessions, key)
	}
	c.mu.Unlock()
}

func (c *Capturer) buildBPFFilter() string {
	var ports []string

	if c.cfg.Capture.Port9100Enabled {
		ports = append(ports, "(tcp port 9100)")
	}
	if c.cfg.Capture.Port515Enabled {
		ports = append(ports, "(tcp port 515)")
	}

	if len(ports) == 1 {
		return ports[0]
	}
	return ports[0] + " or " + ports[1]
}

func (c *Capturer) processPackets() {
	defer c.wg.Done()

	packetSource := gopacket.NewPacketSource(c.handle, c.handle.LinkType())
	packetSource.NoCopy = true

	for {
		select {
		case <-c.done:
			return
		case packet, ok := <-packetSource.Packets():
			if !ok {
				return
			}
			c.handlePacket(packet)
		}
	}
}

func (c *Capturer) handlePacket(packet gopacket.Packet) {
	// Extract network layer
	networkLayer := packet.NetworkLayer()
	if networkLayer == nil {
		return
	}

	var srcIP, dstIP string
	switch nl := networkLayer.(type) {
	case *layers.IPv4:
		srcIP = nl.SrcIP.String()
		dstIP = nl.DstIP.String()
	case *layers.IPv6:
		srcIP = nl.SrcIP.String()
		dstIP = nl.DstIP.String()
	default:
		return
	}

	// Extract TCP layer
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		return
	}
	tcp := tcpLayer.(*layers.TCP)

	srcPort := uint16(tcp.SrcPort)
	dstPort := uint16(tcp.DstPort)

	// Determine direction and port type
	var printerIP, posIP string
	var printerPort uint16
	var transport string
	var isTowardsPrinter bool

	if c.isPrinterPort(dstPort) {
		// Traffic towards printer
		printerIP = dstIP
		printerPort = dstPort
		posIP = srcIP
		transport = c.getTransport(dstPort)
		isTowardsPrinter = true
	} else if c.isPrinterPort(srcPort) {
		// Traffic from printer (ACKs, etc.) - we track but don't capture payload
		printerIP = srcIP
		printerPort = srcPort
		posIP = dstIP
		transport = c.getTransport(srcPort)
		isTowardsPrinter = false
	} else {
		return
	}

	// Create session key (always normalized to printer as destination)
	sessionKey := fmt.Sprintf("%s:%d->%s:%d", posIP, srcPort, printerIP, printerPort)
	if !isTowardsPrinter {
		sessionKey = fmt.Sprintf("%s:%d->%s:%d", posIP, dstPort, printerIP, printerPort)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Handle connection close
	if tcp.FIN || tcp.RST {
		if sess, ok := c.sessions[sessionKey]; ok {
			c.finalizeSession(sess)
			delete(c.sessions, sessionKey)
		}
		return
	}

	// Only capture data going towards printer
	if !isTowardsPrinter {
		return
	}

	// Get or create session
	sess, ok := c.sessions[sessionKey]
	if !ok {
		if tcp.SYN {
			// New connection
			sess = &session{
				job:        job.New(c.cfg.DeviceID, c.cfg.SiteID, printerIP, printerPort, posIP, transport),
				lastSeen:   time.Now(),
				srcIP:      posIP,
				dstIP:      printerIP,
				srcPort:    srcPort,
				dstPort:    printerPort,
				transport:  transport,
				seqTracker: make(map[uint32]bool),
			}
			c.sessions[sessionKey] = sess
			c.logger.Debug("new session",
				"session", sessionKey,
				"job_id", sess.job.Metadata.JobID)
		}
		return
	}

	// Update last seen time
	sess.lastSeen = time.Now()

	// Extract payload
	appLayer := packet.ApplicationLayer()
	if appLayer == nil || len(appLayer.Payload()) == 0 {
		return
	}
	payload := appLayer.Payload()

	// Track sequence numbers for ordering (simplified - assumes in-order delivery for MVP)
	seq := tcp.Seq
	if !sess.initialized {
		sess.nextSeq = seq
		sess.initialized = true
	}

	// Avoid duplicate data
	if sess.seqTracker[seq] {
		return
	}
	sess.seqTracker[seq] = true

	// Append data to job
	if sess.job.Append(payload) {
		c.stats.BytesCaptured.Add(int64(len(payload)))
	}
}

func (c *Capturer) isPrinterPort(port uint16) bool {
	if port == 9100 && c.cfg.Capture.Port9100Enabled {
		return true
	}
	if port == 515 && c.cfg.Capture.Port515Enabled {
		return true
	}
	return false
}

func (c *Capturer) getTransport(port uint16) string {
	switch port {
	case 9100:
		return "tcp9100"
	case 515:
		return "lpd"
	default:
		return "unknown"
	}
}

func (c *Capturer) sessionTimeoutLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.checkTimeouts()
		}
	}
}

func (c *Capturer) checkTimeouts() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, sess := range c.sessions {
		if now.Sub(sess.lastSeen) >= c.cfg.Capture.IdleTimeout {
			c.finalizeSession(sess)
			delete(c.sessions, key)
		}
	}
}

func (c *Capturer) finalizeSession(sess *session) {
	sess.job.Close()

	// Skip empty jobs
	if sess.job.Metadata.ByteLen == 0 {
		c.logger.Debug("skipping empty job",
			"job_id", sess.job.Metadata.JobID)
		return
	}

	// Check for reprint
	if c.reprint != nil {
		originalID := c.reprint.Check(sess.job.GetHash(), sess.dstIP)
		if originalID != "" {
			sess.job.SetReprintOf(originalID)
			c.logger.Info("reprint detected",
				"job_id", sess.job.Metadata.JobID,
				"original_id", originalID)
		}
		c.reprint.Record(sess.job.GetHash(), sess.dstIP, sess.job.Metadata.JobID)
	}

	// Save to disk
	if err := c.store.Save(sess.job); err != nil {
		c.stats.ParseErrors.Add(1)
		c.logger.Error("failed to save job",
			"job_id", sess.job.Metadata.JobID,
			"error", err)
		return
	}

	c.stats.JobsCaptured.Add(1)
	c.logger.Info("job captured",
		"job_id", sess.job.Metadata.JobID,
		"printer_ip", sess.job.Metadata.PrinterIP,
		"src_ip", sess.job.Metadata.SrcIP,
		"bytes", sess.job.Metadata.ByteLen,
		"transport", sess.job.Metadata.Transport)
}

// GetActiveSessions returns the number of active sessions.
func (c *Capturer) GetActiveSessions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions)
}

// FindInterface attempts to find a suitable capture interface.
func FindInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	// Prefer br0 if it exists
	for _, iface := range ifaces {
		if iface.Name == "br0" {
			return "br0", nil
		}
	}

	// Look for any bridge interface
	for _, iface := range ifaces {
		if len(iface.Name) >= 2 && iface.Name[:2] == "br" {
			return iface.Name, nil
		}
	}

	// Fall back to eth0
	for _, iface := range ifaces {
		if iface.Name == "eth0" {
			return "eth0", nil
		}
	}

	return "", fmt.Errorf("no suitable interface found")
}
