# Ubuntu 24.04 Deployment Guide

**Last Updated:** December 2025  
**Target:** Ubuntu 24.04 LTS VPS

---

## Overview

This guide will help you deploy CELLS on an Ubuntu 24.04 VPS. CELLS signs wallet transactions with ML-DSA-87 (NIST FIPS 204, security category 5 post-quantum signatures). The public network is currently a pre-mainnet pilot.

---

## Prerequisites

- Ubuntu 24.04 LTS VPS
- Root or sudo access
- At least 2GB RAM (4GB+ recommended)
- At least 20GB disk space
- Internet connection

---

## Step 1: Initial Server Setup

### Update System

```bash
sudo apt update
sudo apt upgrade -y
```

### Install Essential Tools

```bash
sudo apt install -y \
    build-essential \
    cmake \
    git \
    curl \
    wget \
    vim \
    htop \
    ufw
```

---

## Step 2: Install Go

### Install Go 1.20+

```bash
# Download Go (check latest version at https://go.dev/dl/)
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz

# Remove old Go installation if exists
sudo rm -rf /usr/local/go

# Extract and install
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify installation
go version
```

### Alternative: Install via Package Manager

```bash
sudo apt install -y golang-go
go version
```

---

## Step 3: Install Dependencies

### Install OpenSSL and SQLite Development Headers

```bash
sudo apt install -y \
    libssl-dev \
    libsqlite3-dev \
    pkg-config
```

---

## Step 4: Clone and Build CELLS

### Clone Repository

```bash
cd ~
git clone https://github.com/blackbeardONE/CELLS.git
cd CELLS
```

### Build liboqs (Quantum-Safe Library)

```bash
# Make scripts executable
chmod +x rebuild_liboqs.sh build.sh run.sh

# Build liboqs (takes 10-30 minutes)
./rebuild_liboqs.sh
```

This will:
- Clone and build liboqs
- Enable ML-DSA-87 (256-bit quantum-safe)
- Install to `./liboqs_install/`

### Build CELLS

```bash
# Build CELLS binary
./build.sh
```

This creates the `CELLS` binary in the current directory.

---

## Step 5: Configure CELLS

### Create Configuration File

```bash
# Copy example config
cp config/CELLS.toml.example CELLS.toml

# Edit configuration
vim CELLS.toml
```

**Example `CELLS.toml` for VPS:**

```toml
[network]
port = 4001
bootstrap_peers = []

[storage]
type = "sqlite"
sqlite_path = "/opt/CELLS/CELLS.db"

[monitoring]
dashboard_port = 8081
log_viewer_port = 8080
log_file = "/opt/CELLS/CELLS.log"
log_level = "INFO"

[api]
port = 8443
enable_tls = false  # Set to true if you have certificates

[wallet]
initial_balance = 1000.0

[governance]
proposal_file = "/opt/CELLS/proposals.json"

[performance]
transaction_interval = "30s"
health_check_interval = "30s"
```

---

## Step 6: Production Deployment

### Create CELLS User

```bash
# Create system user
sudo useradd -r -s /bin/false -d /opt/CELLS CELLS

# Create directories
sudo mkdir -p /opt/CELLS
sudo chown CELLS:CELLS /opt/CELLS
```

### Install CELLS Files

```bash
# Copy binary
sudo cp CELLS /opt/CELLS/
sudo chmod +x /opt/CELLS/CELLS

# Copy configuration
sudo cp CELLS.toml /opt/CELLS/

# Copy liboqs libraries
sudo cp -r liboqs_install /opt/CELLS/

# Set ownership
sudo chown -R CELLS:CELLS /opt/CELLS
```

### Create Systemd Service

```bash
# Copy service file
sudo cp config/CELLS.service /etc/systemd/system/

# Edit service file if needed
sudo vim /etc/systemd/system/CELLS.service

# Reload systemd
sudo systemctl daemon-reload

# Enable service (start on boot)
sudo systemctl enable CELLS

# Start service
sudo systemctl start CELLS

# Check status
sudo systemctl status CELLS
```

### View Logs

```bash
# Systemd logs
sudo journalctl -u CELLS -f

# Application logs
sudo tail -f /opt/CELLS/CELLS.log
```

---

## Step 7: Firewall Configuration

### Configure UFW

```bash
# Allow SSH (important!)
sudo ufw allow 22/tcp

# Allow CELLS network port
sudo ufw allow 4001/tcp

# Allow dashboard
sudo ufw allow 8081/tcp

# Allow log viewer
sudo ufw allow 8080/tcp

# Allow API (if using)
sudo ufw allow 8443/tcp

# Enable firewall
sudo ufw enable

# Check status
sudo ufw status
```

---

## Step 8: Verify Deployment

### Check Service Status

```bash
sudo systemctl status CELLS
```

### Check Logs

```bash
# Systemd logs
sudo journalctl -u CELLS --no-pager -n 50

# Application logs
sudo tail -n 50 /opt/CELLS/CELLS.log
```

### Test Endpoints

```bash
# Health check
curl http://localhost:8081/api/health

# Dashboard
curl http://localhost:8081/api/metrics
```

### Access Dashboard

Open in browser:
- **Dashboard:** `http://your-vps-ip:8081`
- **Log Viewer:** `http://your-vps-ip:8080`

---

## Step 9: Maintenance

### Restart Service

```bash
sudo systemctl restart CELLS
```

### Stop Service

```bash
sudo systemctl stop CELLS
```

### Update CELLS

```bash
# Stop service
sudo systemctl stop CELLS

# Backup current installation
sudo cp -r /opt/CELLS /opt/CELLS.backup

# Update code
cd ~/CELLS
git pull

# Rebuild
./rebuild_liboqs.sh  # Only if liboqs changed
./build.sh

# Install new binary
sudo cp CELLS /opt/CELLS/
sudo chown CELLS:CELLS /opt/CELLS/CELLS

# Start service
sudo systemctl start CELLS

# Verify
sudo systemctl status CELLS
```

### View Metrics

```bash
# Via API
curl http://localhost:8081/api/metrics | jq

# Via dashboard
# Open http://your-vps-ip:8081 in browser
```

---

## Troubleshooting

### Service Won't Start

**Check logs:**
```bash
sudo journalctl -u CELLS -n 100
```

**Common issues:**
1. **Missing liboqs:** Ensure `LD_LIBRARY_PATH` is set in service file
2. **Port in use:** Change port in `CELLS.toml`
3. **Permission denied:** Check file ownership (`sudo chown -R CELLS:CELLS /opt/CELLS`)

### liboqs Not Found

**Check library path:**
```bash
# Find liboqs
find /opt/CELLS -name "liboqs.so*"

# Update service file
sudo vim /etc/systemd/system/CELLS.service
# Update LD_LIBRARY_PATH
```

### High Memory Usage

**Monitor resources:**
```bash
htop
```

**Adjust log level:**
```toml
[monitoring]
log_level = "WARN"  # Reduce logging
```

### Database Issues

**Check database:**
```bash
sudo -u CELLS sqlite3 /opt/CELLS/CELLS.db ".tables"
```

**Backup database:**
```bash
sudo -u CELLS cp /opt/CELLS/CELLS.db /opt/CELLS/CELLS.db.backup
```

---

## Security Recommendations

### 1. Use TLS for API

```toml
[api]
enable_tls = true
tls_cert_file = "/etc/ssl/certs/CELLS.crt"
tls_key_file = "/etc/ssl/private/CELLS.key"
```

### 2. Restrict Dashboard Access

Use reverse proxy (nginx) with authentication:
```nginx
location / {
    auth_basic "CELLS Dashboard";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://localhost:8081;
}
```

### 3. Regular Updates

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Update CELLS
cd ~/CELLS && git pull && ./build.sh
```

### 4. Monitor Logs

```bash
# Set up log rotation
sudo vim /etc/logrotate.d/CELLS
```

---

## Performance Tuning

### Increase File Descriptors

```bash
# Edit limits
sudo vim /etc/security/limits.conf
# Add:
CELLS soft nofile 65536
CELLS hard nofile 65536
```

### Optimize SQLite

The service file already includes optimizations. For custom tuning, edit `CELLS.toml`.

---

## Backup Strategy

### Automated Backup Script

Create `/opt/CELLS/backup.sh`:

```bash
#!/bin/bash
BACKUP_DIR="/opt/CELLS/backups"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# Backup database
cp /opt/CELLS/CELLS.db "$BACKUP_DIR/CELLS_$DATE.db"

# Backup configuration
cp /opt/CELLS/CELLS.toml "$BACKUP_DIR/CELLS_$DATE.toml"

# Keep only last 7 days
find "$BACKUP_DIR" -name "CELLS_*.db" -mtime +7 -delete
find "$BACKUP_DIR" -name "CELLS_*.toml" -mtime +7 -delete
```

**Add to crontab:**
```bash
sudo crontab -e
# Add:
0 2 * * * /opt/CELLS/backup.sh
```

---

## Monitoring

### System Monitoring

```bash
# Install monitoring tools
sudo apt install -y prometheus-node-exporter

# Or use CELLS's built-in dashboard
# http://your-vps-ip:8081
```

### Alerting

Set up alerts for:
- Service down
- High memory usage
- Disk space low
- High error rate

---

## Next Steps

- **Multi-node setup:** Configure bootstrap peers
- **Load balancing:** Use nginx as reverse proxy
- **Monitoring:** Integrate with Prometheus/Grafana
- **Backup:** Set up automated backups

---

## Support

For issues or questions:
- Check logs: `sudo journalctl -u CELLS`
- Review documentation: `docs/`
- GitHub Issues: [CELLS Issues](https://github.com/blackbeardONE/CELLS/issues)

---

*Happy deploying! 🚀*

