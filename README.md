# Kitchen Printer Tap

A transparent network tap for capturing kitchen printer jobs from Vectron and TCPOS systems. The device sits inline between the network switch and printer, forwarding all traffic unchanged while capturing print jobs for analytics.

## Key Features

- **Transparent bridging**: Printer keeps its existing IP, no POS configuration changes needed
- **100% reliable printing**: Linux bridge forwards traffic even if capture service fails
- **Passive capture**: Read-only packet capture, no traffic modification
- **Local storage**: Jobs stored locally with metadata, survives network outages
- **Optional webhook upload**: Asynchronous upload with retry logic

## Target Hardware

- **Primary**: Revolution Pi Connect (two Ethernet ports, Debian-based)
- **Fallback**: Any Linux system with two NICs (Intel NUC, Raspberry Pi 4, etc.)

## Quick Start

### Prerequisites

- Debian/Ubuntu-based Linux
- Two Ethernet interfaces (eth0, eth1)
- Go 1.22+ (for building)
- libpcap-dev
- bridge-utils

### Installation

```bash
# Clone repository
git clone https://github.com/marcenggist/kitchen-printer-tap.git
cd kitchen-printer-tap

# Install build dependencies
sudo apt-get update
sudo apt-get install -y golang libpcap-dev bridge-utils

# Build
make build

# Install (creates user, directories, systemd service)
sudo make install

# Configure
sudo nano /etc/kitchen-printer-tap/config.yaml

# Set up bridge (with persistence)
sudo ./scripts/setup-bridge.sh --persist

# Start service
sudo systemctl enable kitchen-printer-tap
sudo systemctl start kitchen-printer-tap
```

### Verify Installation

```bash
# Check bridge status
ip link show br0
bridge link show

# Check service status
sudo systemctl status kitchen-printer-tap

# View logs
sudo journalctl -u kitchen-printer-tap -f

# Health check
curl http://127.0.0.1:8088/health
```

### Test Print Capture

```bash
# Watch traffic on bridge
sudo tcpdump -i br0 -n port 9100

# Send a print job from POS and verify:
# 1. Print appears on kitchen printer
# 2. Job file created in storage directory
ls /var/lib/kitchen-printer-tap/$(date +%Y)/$(date +%m)/$(date +%d)/
```

## Network Topology

```
Before (direct connection):
┌────────┐          ┌──────────────┐
│ Switch │──────────│ Kitchen      │
│ / POS  │          │ Printer      │
└────────┘          └──────────────┘

After (with tap device):
┌────────┐          ┌──────────────┐          ┌──────────────┐
│ Switch │──eth0────│ Kitchen      │──eth1────│ Kitchen      │
│ / POS  │          │ Printer Tap  │          │ Printer      │
└────────┘          │   (br0)      │          └──────────────┘
                    └──────────────┘
```

## Configuration

Edit `/etc/kitchen-printer-tap/config.yaml`:

```yaml
# Device identification
device_id: "kptap-001"
site_id: "site-001"

# Capture interface (bridge)
interface: "br0"

# Capture settings
capture:
  port_9100_enabled: true   # Raw printing (most common)
  port_515_enabled: false   # LPD (optional)
  idle_timeout: 800ms       # Job boundary detection

# Storage settings
storage:
  base_path: "/var/lib/kitchen-printer-tap"
  min_free_mb: 100

# Optional webhook upload
upload:
  enabled: false
  webhook_url: "https://api.example.com/print-jobs"
  auth_token: "your-token-here"
```

## Data Output

For each captured print job:

**Binary file** (`{job_id}.bin`): Raw payload bytes

**Metadata file** (`{job_id}.json`):
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "device_id": "kptap-001",
  "site_id": "site-001",
  "printer_ip": "192.168.1.50",
  "printer_port": 9100,
  "src_ip": "192.168.1.10",
  "capture_start_ts": "2024-01-15T14:30:00Z",
  "capture_end_ts": "2024-01-15T14:30:01Z",
  "byte_len": 4523,
  "sha256": "abc123...",
  "transport": "tcp9100",
  "tags": []
}
```

Files are organized by date: `/var/lib/kitchen-printer-tap/YYYY/MM/DD/`

## Commands Reference

### Service Management

```bash
# Start/stop/restart
sudo systemctl start kitchen-printer-tap
sudo systemctl stop kitchen-printer-tap
sudo systemctl restart kitchen-printer-tap

# View status
sudo systemctl status kitchen-printer-tap

# Enable at boot
sudo systemctl enable kitchen-printer-tap
```

### Logging

```bash
# Live logs
sudo journalctl -u kitchen-printer-tap -f

# Last 100 lines
sudo journalctl -u kitchen-printer-tap -n 100

# Errors only
sudo journalctl -u kitchen-printer-tap -p err
```

### Bridge Management

```bash
# Check bridge status
ip link show br0
bridge link show

# View bridge forwarding database
bridge fdb show br br0

# Check interfaces
ip -br link show
```

### Network Debugging

```bash
# Capture port 9100 traffic
sudo tcpdump -i br0 -n port 9100

# Save capture to file
sudo tcpdump -i br0 -n port 9100 -w /tmp/print.pcap

# Show packet contents
sudo tcpdump -i br0 -n port 9100 -X
```

### Health Monitoring

```bash
# Health endpoint
curl -s http://127.0.0.1:8088/health | jq .

# Count jobs captured today
ls /var/lib/kitchen-printer-tap/$(date +%Y)/$(date +%m)/$(date +%d)/*.json 2>/dev/null | wc -l
```

## Rollback

If you need to remove the tap device and restore direct printer connection:

```bash
# Stop service
sudo systemctl stop kitchen-printer-tap

# Remove bridge
sudo ./scripts/rollback-bridge.sh --remove-persistent

# Reconnect printer directly to switch
```

## Troubleshooting

See [docs/troubleshooting.txt](docs/troubleshooting.txt) for detailed troubleshooting guide.

### Common Issues

**Printing not working after install**
```bash
# Immediate rollback
sudo systemctl stop kitchen-printer-tap
sudo ./scripts/rollback-bridge.sh
```

**No jobs being captured**
```bash
# Verify traffic on bridge
sudo tcpdump -i br0 -n port 9100

# Check service logs
sudo journalctl -u kitchen-printer-tap -n 50
```

**tapd won't start**
```bash
# Check logs for error
sudo journalctl -u kitchen-printer-tap -n 50

# Verify bridge exists
ip link show br0

# Verify capabilities
getcap /usr/local/bin/tapd
```

## Security

- Service runs as dedicated `kptap` user
- Config file permissions: 640 (root:kptap)
- Health endpoint only on localhost (127.0.0.1)
- Webhook uses TLS
- No PII parsing in MVP

## Project Structure

```
kitchen-printer-tap/
├── cmd/tapd/              # Main application
│   └── main.go
├── internal/
│   ├── capture/           # Packet capture and reassembly
│   ├── config/            # Configuration loading
│   ├── health/            # Health endpoint
│   ├── job/               # Job storage and metadata
│   └── upload/            # Webhook upload worker
├── scripts/
│   ├── setup-bridge.sh    # Configure Linux bridge
│   ├── rollback-bridge.sh # Remove bridge
│   └── install.sh         # Install service
├── configs/
│   └── config.yaml        # Example configuration
├── systemd/
│   └── kitchen-printer-tap.service
├── docs/
│   ├── wiring.txt         # Physical installation guide
│   ├── test-plan.txt      # Acceptance tests
│   └── troubleshooting.txt
├── Makefile
├── go.mod
└── README.md
```

## Building

```bash
# Build binary
make build

# Build with version info
make build VERSION=1.0.0

# Run tests
make test

# Clean build artifacts
make clean
```

## License

Proprietary - Fairmont Hotels & Resorts

## Support

For issues and questions, contact the development team or open an issue in the repository.
