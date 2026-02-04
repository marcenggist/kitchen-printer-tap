# Kitchen Printer Tap - Product Requirements Document

**Version:** 1.0
**Status:** MVP
**Last Updated:** 2026-02-04
**Author:** EGS Engineering

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Goals and Objectives](#2-goals-and-objectives)
3. [MVP Scope](#3-mvp-scope)
4. [Non-Goals (Out of Scope)](#4-non-goals-out-of-scope)
5. [Target Hardware](#5-target-hardware)
6. [Network Topology](#6-network-topology)
7. [Data Output Format](#7-data-output-format)
8. [Job Boundary Detection](#8-job-boundary-detection)
9. [Upload Specifications](#9-upload-specifications)
10. [Acceptance Criteria](#10-acceptance-criteria)
11. [Edge Cases and Error Handling](#11-edge-cases-and-error-handling)
12. [Security Considerations](#12-security-considerations)
13. [Future Enhancements](#13-future-enhancements)

---

## 1. Project Overview

### 1.1 Problem Statement

Restaurant and hotel kitchens using POS systems (Vectron, TCPOS) print orders to kitchen printers via direct network connections. Currently, there is no visibility into:
- What orders were printed
- When orders were printed
- Whether reprints occurred
- Historical print data for analytics

Modifying POS software or printer firmware is not feasible due to:
- Vendor lock-in and support contracts
- Certification requirements (PCI-DSS, etc.)
- Risk of disrupting production systems

### 1.2 Solution

A **transparent network tap device** that sits inline between the network switch and kitchen printer. The device:
- Forwards all traffic unchanged (printer works normally)
- Passively captures print job data
- Stores captures locally with metadata
- Optionally uploads to a webhook for analytics

### 1.3 Key Principles

| Principle | Description |
|-----------|-------------|
| **Transparency** | Printer keeps existing IP, no POS changes required |
| **Reliability** | Printing works even if capture service fails |
| **Passive** | Read-only capture, no traffic modification |
| **Resilient** | Local storage survives network outages |

---

## 2. Goals and Objectives

### 2.1 Primary Goals

1. **Zero Impact on Printing** - Printing must work exactly as before, with no latency or failures introduced by the tap device
2. **Complete Capture** - Every print job must be captured with full payload data
3. **Offline Operation** - Device must function without internet connectivity
4. **Easy Installation** - Physical installation under 15 minutes, no POS configuration changes

### 2.2 Success Metrics

| Metric | Target |
|--------|--------|
| Print success rate with tap inline | 100% |
| Print latency introduced | < 1ms |
| Job capture rate | 100% |
| Uptime | 99.9% |
| Mean time to install | < 15 minutes |

---

## 3. MVP Scope

### 3.1 Included in MVP

| Feature | Description |
|---------|-------------|
| Transparent bridging | Linux bridge forwards all traffic between eth0 and eth1 |
| Port 9100 capture | Raw socket printing (most common) |
| Port 515 capture | LPD protocol (optional, configurable) |
| TCP stream reassembly | Reconstruct complete print jobs from packets |
| Job boundary detection | Detect job start/end via idle timeout |
| Local file storage | Store jobs as .bin (payload) + .json (metadata) |
| Date-based directories | `/var/lib/kitchen-printer-tap/YYYY/MM/DD/` |
| Reprint detection | Identify duplicate prints via SHA256 hash |
| Health endpoint | HTTP `/health` for monitoring |
| Systemd service | Auto-start, restart on failure |
| Setup/rollback scripts | Easy bridge configuration |

### 3.2 Configuration Options

```yaml
# Capture settings
capture:
  port_9100_enabled: true      # Raw printing
  port_515_enabled: false      # LPD (optional)
  idle_timeout: 800ms          # Job boundary detection

# Storage settings
storage:
  base_path: "/var/lib/kitchen-printer-tap"
  min_free_mb: 100             # Stop if disk < 100MB
  retention_days: 30           # For cleanup scripts
  reprint_window_sec: 300      # Reprint detection window

# Upload settings (optional)
upload:
  enabled: false
  webhook_url: ""
  auth_token: ""
  max_retries: 3
```

---

## 4. Non-Goals (Out of Scope)

The following are explicitly **NOT** part of the MVP:

| Non-Goal | Rationale |
|----------|-----------|
| Print job parsing/interpretation | Raw binary capture only; parsing is done server-side |
| Print job modification | Read-only capture, never alter traffic |
| Web UI on device | CLI and config files only; UI is server-side |
| Multi-printer support | One tap per printer; scale horizontally |
| Wireless connectivity | Wired Ethernet only for reliability |
| Print job blocking/filtering | All jobs pass through unchanged |
| Real-time streaming | Store-and-forward only |
| Windows/macOS support | Linux only (Debian-based) |

---

## 5. Target Hardware

### 5.1 Primary Target: Revolution Pi Connect

| Specification | Value |
|---------------|-------|
| Processor | ARM Cortex-A53 (Broadcom BCM2837) |
| RAM | 1 GB |
| Storage | 8 GB eMMC (expandable via SD) |
| Ethernet | 2x Gigabit Ethernet (critical requirement) |
| Power | 24V DC |
| OS | Debian-based (Raspbian derivative) |
| Form Factor | DIN rail mountable |

**Why Revolution Pi:**
- Industrial-grade hardware designed for 24/7 operation
- Two built-in Ethernet ports (no USB adapters)
- DIN rail mounting fits electrical cabinets
- Widely available, good support

### 5.2 Alternative Hardware

Any Linux system with two network interfaces:

| Device | Pros | Cons |
|--------|------|------|
| Raspberry Pi 4 + USB-Ethernet | Low cost, widely available | USB adapter less reliable |
| Intel NUC + USB-Ethernet | More powerful | Overkill, higher cost |
| Standard PC with dual NIC | Most flexible | Large form factor |

### 5.3 Minimum Requirements

- 2x Ethernet interfaces (built-in or USB)
- 512 MB RAM
- 4 GB storage
- Debian/Ubuntu Linux
- libpcap support

---

## 6. Network Topology

### 6.1 Before Installation

```
┌─────────────────┐                    ┌─────────────────┐
│                 │     Ethernet       │                 │
│  POS Terminal   │───────────────────>│ Kitchen Printer │
│  or Switch      │                    │ (192.168.1.50)  │
│                 │                    │                 │
└─────────────────┘                    └─────────────────┘
```

### 6.2 After Installation

```
┌─────────────────┐          ┌─────────────────────┐          ┌─────────────────┐
│                 │   eth0   │                     │   eth1   │                 │
│  POS Terminal   │─────────>│  Kitchen Printer    │─────────>│ Kitchen Printer │
│  or Switch      │          │  Tap Device         │          │ (192.168.1.50)  │
│                 │          │                     │          │                 │
└─────────────────┘          │  ┌───────────────┐  │          └─────────────────┘
                             │  │   br0 bridge  │  │
                             │  │   (passive)   │  │
                             │  └───────────────┘  │
                             │         │          │
                             │    ┌────┴────┐     │
                             │    │  tapd   │     │
                             │    │ capture │     │
                             │    └─────────┘     │
                             └─────────────────────┘
```

### 6.3 Bridge Behavior

| Scenario | Behavior |
|----------|----------|
| tapd running | Traffic forwarded AND captured |
| tapd stopped | Traffic forwarded only (printing works) |
| tapd crashed | Traffic forwarded only (auto-restart via systemd) |
| Device powered off | No forwarding (printer offline) |

**Critical Design Decision:** The Linux bridge operates at Layer 2, independent of the tapd application. This ensures printing continues even if the capture service fails.

---

## 7. Data Output Format

### 7.1 File Structure

```
/var/lib/kitchen-printer-tap/
└── 2026/
    └── 02/
        └── 04/
            ├── 550e8400-e29b-41d4-a716-446655440000.bin   # Raw payload
            ├── 550e8400-e29b-41d4-a716-446655440000.json  # Metadata
            ├── 6fa459ea-ee8a-3ca4-894e-db77e160355e.bin
            └── 6fa459ea-ee8a-3ca4-894e-db77e160355e.json
```

### 7.2 Metadata JSON Schema

```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "device_id": "kptap-001",
  "site_id": "site-fairmont-kitchen-1",
  "printer_ip": "192.168.1.50",
  "printer_port": 9100,
  "src_ip": "192.168.1.10",
  "capture_start_ts": "2026-02-04T14:23:45.123456Z",
  "capture_end_ts": "2026-02-04T14:23:45.892123Z",
  "byte_len": 4523,
  "sha256": "a1b2c3d4e5f6...",
  "transport": "tcp",
  "tags": ["reprint"],
  "reprint_of_job_id": "6fa459ea-ee8a-3ca4-894e-db77e160355e"
}
```

### 7.3 Field Definitions

| Field | Type | Description |
|-------|------|-------------|
| `job_id` | UUID | Unique identifier for this capture |
| `device_id` | string | Configured device identifier |
| `site_id` | string | Configured site/location identifier |
| `printer_ip` | string | Destination IP (printer) |
| `printer_port` | uint16 | Destination port (9100 or 515) |
| `src_ip` | string | Source IP (POS terminal) |
| `capture_start_ts` | ISO8601 | First packet timestamp (UTC) |
| `capture_end_ts` | ISO8601 | Last packet + idle timeout (UTC) |
| `byte_len` | int | Total bytes in .bin file |
| `sha256` | string | SHA256 hash of payload |
| `transport` | string | Protocol: "tcp" |
| `tags` | array | Optional tags: ["reprint"] |
| `reprint_of_job_id` | string | If reprint, references original job |

### 7.4 Binary Payload (.bin)

- Raw TCP payload data
- Concatenated in sequence order
- No headers or framing added
- Exact bytes sent to printer

---

## 8. Job Boundary Detection

### 8.1 The Problem

Print jobs arrive as a stream of TCP packets. There is no explicit "job start" or "job end" marker in the protocol. We must infer job boundaries from traffic patterns.

### 8.2 Idle Timeout Method

**Algorithm:**
1. First packet to printer:port creates a new session/job
2. Each subsequent packet resets the idle timer
3. When idle timer expires, job is considered complete
4. Session is closed, files are written

**Default Timeout:** 800ms

**Rationale:**
- Kitchen receipts are small (< 10KB typically)
- Transmission completes in < 100ms on local network
- 800ms provides margin for slow printers or network issues
- Fast enough to separate rapid successive prints

### 8.3 TCP Stream Reassembly

- Track TCP sequence numbers to detect retransmissions
- Deduplicate retransmitted packets
- Handle out-of-order packets (reorder by sequence)
- Detect and handle connection resets

### 8.4 Session Key

Sessions are identified by the tuple:
```
(src_ip, src_port, dst_ip, dst_port)
```

This allows multiple concurrent connections to be tracked separately.

---

## 9. Upload Specifications

### 9.1 Overview

Upload is **optional** and disabled by default. When enabled, completed jobs are queued for asynchronous upload to a webhook endpoint.

### 9.2 Upload Flow

```
Job Captured → Queue → Upload Worker → Webhook
                 ↓
            Retry on failure
                 ↓
            Max retries → Log and skip
```

### 9.3 Webhook Request Format

**Endpoint:** Configured `webhook_url`

**Method:** POST

**Headers:**
```
Content-Type: application/json
Authorization: Bearer {auth_token}
X-Device-ID: {device_id}
X-Site-ID: {site_id}
```

**Body:**
```json
{
  "jobs": [
    {
      "metadata": { /* same as .json file */ },
      "payload_base64": "SGVsbG8gV29ybGQ..."
    }
  ]
}
```

### 9.4 Retry Logic

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_retries` | 3 | Maximum retry attempts per job |
| `retry_backoff` | 5s | Wait time between retries |
| `timeout` | 30s | HTTP request timeout |
| `batch_size` | 10 | Jobs per request |

### 9.5 Failure Handling

- Jobs remain on disk regardless of upload status
- Failed uploads are logged with error details
- No automatic retry after max_retries exhausted
- Manual re-upload possible via future CLI tool

---

## 10. Acceptance Criteria

### 10.1 Functional Requirements

| ID | Requirement | Verification |
|----|-------------|--------------|
| F1 | Printing works with tapd stopped | Test: Stop tapd, print 10 jobs, all succeed |
| F2 | Printing works with tapd running | Test: Start tapd, print 10 jobs, all succeed |
| F3 | All print jobs are captured | Test: Print 10 jobs, count 10 .json files |
| F4 | Metadata matches actual payload | Test: byte_len equals .bin file size |
| F5 | Reprint detection works | Test: Print same job twice, second has "reprint" tag |
| F6 | Health endpoint responds | Test: curl /health returns 200 with valid JSON |
| F7 | Service auto-restarts | Test: kill -9 tapd, verify restart within 10s |
| F8 | Rollback script works | Test: Run rollback, verify bridge removed |

### 10.2 Performance Requirements

| ID | Requirement | Target |
|----|-------------|--------|
| P1 | Print latency added | < 1ms |
| P2 | CPU usage during capture | < 10% |
| P3 | Memory usage | < 100 MB |
| P4 | Burst handling | 50 jobs/minute without loss |
| P5 | Disk write speed | Not bottleneck for typical prints |

### 10.3 Reliability Requirements

| ID | Requirement | Target |
|----|-------------|--------|
| R1 | Printing availability | 100% (bridge independent of tapd) |
| R2 | Capture availability | 99.9% (auto-restart on failure) |
| R3 | Data durability | Atomic writes, no partial files |
| R4 | Network outage resilience | Full offline operation |

---

## 11. Edge Cases and Error Handling

### 11.1 Network Edge Cases

| Scenario | Handling |
|----------|----------|
| POS sends job during tapd restart | Bridge forwards (job printed), capture missed |
| Printer offline/unreachable | Bridge attempt forwarding, POS sees error |
| Very large print job (> 1MB) | Capture continues, limited by disk space |
| Rapid successive jobs (< 800ms apart) | May merge into single capture |
| Connection reset mid-job | Close job with data received so far |

### 11.2 Storage Edge Cases

| Scenario | Handling |
|----------|----------|
| Disk full (< min_free_mb) | Stop writing, log error, continue forwarding |
| Write permission denied | Log error, continue forwarding |
| Clock sync issues | Use monotonic clock for timeouts, UTC for timestamps |

### 11.3 Service Edge Cases

| Scenario | Handling |
|----------|----------|
| tapd crash | Systemd auto-restart (Restart=always) |
| tapd hung | Watchdog timeout, systemd restart |
| Config file missing | Exit with clear error message |
| Config file invalid | Exit with validation error |
| Interface not found | Exit with clear error message |

### 11.4 Protocol Edge Cases

| Scenario | Handling |
|----------|----------|
| Non-print traffic on port 9100 | Capture anyway (server can filter) |
| Encrypted print data | Capture encrypted bytes (no decryption) |
| Fragmented packets | Reassemble via TCP stream |
| Duplicate packets | Deduplicate via sequence tracking |

---

## 12. Security Considerations

### 12.1 Network Security

- Device has no public IP (bridge is transparent)
- Health endpoint binds to 127.0.0.1 only
- No inbound ports exposed
- Outbound only for webhook upload (if enabled)

### 12.2 Access Control

- Runs as dedicated `kptap` user (not root after init)
- Minimal filesystem permissions
- Config file readable by kptap only
- Storage directory owned by kptap

### 12.3 Data Security

- Print data may contain sensitive info (orders, prices)
- Local storage has restricted permissions (0640)
- Webhook upload uses TLS (HTTPS required)
- Auth token for webhook authentication

### 12.4 Systemd Hardening

```ini
[Service]
User=kptap
Group=kptap
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/kitchen-printer-tap
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
```

---

## 13. Future Enhancements

The following are potential future enhancements, **not part of MVP**:

### 13.1 Phase 2 Candidates

| Feature | Description | Priority |
|---------|-------------|----------|
| USB printer support | Capture USB print traffic | Medium |
| Print job parsing | Extract text/items from receipts | High |
| Web dashboard | Local web UI for status/config | Low |
| Multi-printer | Single device capturing multiple printers | Medium |
| Compression | Compress stored payloads | Low |
| Encryption at rest | Encrypt stored data | Medium |

### 13.2 Integration Possibilities

| Integration | Description |
|-------------|-------------|
| CalcMenu | Send captured data to CalcMenu for analytics |
| Kitchen Display | Trigger KDS updates from print captures |
| Inventory | Track items ordered for inventory management |
| Analytics | Dashboard for print volume, peak times, reprints |

---

## Appendix A: Glossary

| Term | Definition |
|------|------------|
| **Bridge** | Linux network bridge (br0) connecting eth0 and eth1 |
| **BPF** | Berkeley Packet Filter - kernel-level packet filtering |
| **Idle Timeout** | Time after last packet before considering job complete |
| **LPD** | Line Printer Daemon protocol (port 515) |
| **Raw Printing** | Direct socket printing on port 9100 |
| **Reprint** | Duplicate print job detected via hash match |
| **Session** | Single TCP connection being tracked |
| **Tap** | Passive network monitoring device |
| **tapd** | The Kitchen Printer Tap daemon process |

---

## Appendix B: References

- [gopacket Documentation](https://pkg.go.dev/github.com/google/gopacket)
- [Linux Bridge Administration](https://wiki.linuxfoundation.org/networking/bridge)
- [Revolution Pi Hardware](https://revolutionpi.com/revolution-pi-connect/)
- [Raw Socket Printing (Port 9100)](https://en.wikipedia.org/wiki/JetDirect)
- [LPD Protocol (RFC 1179)](https://datatracker.ietf.org/doc/html/rfc1179)

---

## Document History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-02-04 | EGS Engineering | Initial PRD for MVP |
