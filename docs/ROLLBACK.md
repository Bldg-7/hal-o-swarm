# HAL-O-SWARM Rollback Procedures

**Version 1.0** | February 2026

Safe procedures for rolling back HAL-O-SWARM deployments and recovering from failed updates.

## Table of Contents

1. [Pre-Rollback Checklist](#pre-rollback-checklist)
2. [Version Rollback](#version-rollback)
3. [Configuration Rollback](#configuration-rollback)
4. [Data Recovery](#data-recovery)
5. [Verification](#verification)

---

## Pre-Rollback Checklist

Before initiating any rollback:

- [ ] Identify the issue and confirm rollback is necessary
- [ ] Notify stakeholders in #incidents Discord channel
- [ ] Collect current logs: `sudo journalctl -u hal-supervisor -n 1000 > /tmp/supervisor.log`
- [ ] Verify backups exist: `ls -lh /var/lib/hal-o-swarm/supervisor.db.backup*`
- [ ] Estimate rollback time and impact
- [ ] Have rollback plan documented before starting
- [ ] Ensure at least one team member is available to monitor

---

## Version Rollback

### Rollback Supervisor Binary

**Scenario**: New supervisor version has critical bug

**Procedure**:

```bash
# 1. Stop supervisor
sudo systemctl stop hal-supervisor

# 2. Verify backup binary exists
ls -lh /usr/local/bin/hal-supervisor.backup

# 3. Restore previous binary
sudo cp /usr/local/bin/hal-supervisor.backup /usr/local/bin/hal-supervisor

# 4. Verify binary is executable
sudo ls -lh /usr/local/bin/hal-supervisor

# 5. Start supervisor
sudo systemctl start hal-supervisor

# 6. Verify it started
sudo systemctl status hal-supervisor

# 7. Check logs for errors
sudo journalctl -u hal-supervisor --since "5 minutes ago"
```

**Backup Strategy**:

Before updating supervisor, create backup:

```bash
# Before update
sudo cp /usr/local/bin/hal-supervisor /usr/local/bin/hal-supervisor.backup

# Update supervisor
curl -fsSL https://raw.githubusercontent.com/Bldg-7/hal-o-swarm/main/deploy/install-release.sh | sudo bash -s -- --supervisor

# If update fails, restore backup
sudo cp /usr/local/bin/hal-supervisor.backup /usr/local/bin/hal-supervisor
sudo systemctl restart hal-supervisor
```

### Rollback Agent Binary

**Scenario**: New agent version has critical bug

**Procedure**:

```bash
# 1. Stop agent
sudo systemctl stop hal-agent

# 2. Verify backup binary exists
ls -lh /usr/local/bin/hal-agent.backup

# 3. Restore previous binary
sudo cp /usr/local/bin/hal-agent.backup /usr/local/bin/hal-agent

# 4. Verify binary is executable
sudo ls -lh /usr/local/bin/hal-agent

# 5. Start agent
sudo systemctl start hal-agent

# 6. Verify it started
sudo systemctl status hal-agent

# 7. Check logs for errors
sudo journalctl -u hal-agent --since "5 minutes ago"
```

**Backup Strategy**:

Before updating agent, create backup on each node:

```bash
# Before update
sudo cp /usr/local/bin/hal-agent /usr/local/bin/hal-agent.backup

# Update agent
curl -fsSL https://raw.githubusercontent.com/Bldg-7/hal-o-swarm/main/deploy/install-release.sh | sudo bash -s -- --agent

# If update fails, restore backup
sudo cp /usr/local/bin/hal-agent.backup /usr/local/bin/hal-agent
sudo systemctl restart hal-agent
```

### Rollback Multiple Agents

**Scenario**: Need to rollback agents across multiple nodes

**Procedure**:

```bash
# 1. Create rollback script
cat > /tmp/rollback-agents.sh << 'EOF'
#!/bin/bash
set -e

AGENTS=("agent1.example.com" "agent2.example.com" "agent3.example.com")

for agent in "${AGENTS[@]}"; do
    echo "Rolling back $agent..."
    ssh "$agent" "sudo systemctl stop hal-agent"
    ssh "$agent" "sudo cp /usr/local/bin/hal-agent.backup /usr/local/bin/hal-agent"
    ssh "$agent" "sudo systemctl start hal-agent"
    ssh "$agent" "sudo systemctl status hal-agent"
    echo "✓ $agent rolled back"
done

echo "All agents rolled back successfully"
EOF

# 2. Run rollback script
bash /tmp/rollback-agents.sh

# 3. Verify all agents are connected
halctl --supervisor-url ws://localhost:8420 nodes list
```

---

## Configuration Rollback

### Rollback Supervisor Configuration

**Scenario**: Configuration change broke supervisor

**Procedure**:

```bash
# 1. Identify the problematic configuration
sudo nano /etc/hal-o-swarm/supervisor.config.json

# 2. Verify backup exists
ls -lh /etc/hal-o-swarm/supervisor.config.json.backup

# 3. Restore previous configuration
sudo cp /etc/hal-o-swarm/supervisor.config.json.backup \
  /etc/hal-o-swarm/supervisor.config.json

# 4. Validate configuration
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# 5. Restart supervisor
sudo systemctl restart hal-supervisor

# 6. Verify it started
sudo systemctl status hal-supervisor

# 7. Check logs for errors
sudo journalctl -u hal-supervisor --since "5 minutes ago"
```

**Backup Strategy**:

Before modifying configuration, create backup:

```bash
# Before modification
sudo cp /etc/hal-o-swarm/supervisor.config.json \
  /etc/hal-o-swarm/supervisor.config.json.backup.$(date +%s)

# Make changes
sudo nano /etc/hal-o-swarm/supervisor.config.json

# Validate changes
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# If validation fails, restore backup
sudo cp /etc/hal-o-swarm/supervisor.config.json.backup.$(date +%s) \
  /etc/hal-o-swarm/supervisor.config.json
```

### Rollback Agent Configuration

**Scenario**: Configuration change broke agent

**Procedure**:

```bash
# 1. Identify the problematic configuration
sudo nano /etc/hal-o-swarm/agent.config.json

# 2. Verify backup exists
ls -lh /etc/hal-o-swarm/agent.config.json.backup

# 3. Restore previous configuration
sudo cp /etc/hal-o-swarm/agent.config.json.backup \
  /etc/hal-o-swarm/agent.config.json

# 4. Validate configuration
/usr/local/bin/hal-agent --config /etc/hal-o-swarm/agent.config.json --validate

# 5. Restart agent
sudo systemctl restart hal-agent

# 6. Verify it started
sudo systemctl status hal-agent

# 7. Check logs for errors
sudo journalctl -u hal-agent --since "5 minutes ago"
```

---

## Data Recovery

### Restore from Database Backup

**Scenario**: Database corruption or accidental data deletion

**Procedure**:

```bash
# 1. Stop supervisor
sudo systemctl stop hal-supervisor

# 2. Backup corrupted database
sudo cp /var/lib/hal-o-swarm/supervisor.db \
  /var/lib/hal-o-swarm/supervisor.db.corrupted.$(date +%s)

# 3. Verify backup exists
ls -lh /var/lib/hal-o-swarm/supervisor.db.backup*

# 4. Restore from backup
sudo cp /var/lib/hal-o-swarm/supervisor.db.backup \
  /var/lib/hal-o-swarm/supervisor.db

# 5. Verify permissions
sudo chown hal-supervisor:hal-supervisor /var/lib/hal-o-swarm/supervisor.db
sudo chmod 600 /var/lib/hal-o-swarm/supervisor.db

# 6. Start supervisor
sudo systemctl start hal-supervisor

# 7. Verify it started
sudo systemctl status hal-supervisor

# 8. Check logs for errors
sudo journalctl -u hal-supervisor --since "5 minutes ago"

# 9. Verify data is restored
halctl --supervisor-url ws://localhost:8420 sessions list
```

### Restore from Point-in-Time Backup

**Scenario**: Need to restore to specific point in time

**Procedure**:

```bash
# 1. List available backups
ls -lh /backup/supervisor.db.*

# 2. Identify desired backup
# Format: supervisor.db.YYYYMMDD
BACKUP_DATE="20260215"

# 3. Stop supervisor
sudo systemctl stop hal-supervisor

# 4. Backup current database
sudo cp /var/lib/hal-o-swarm/supervisor.db \
  /var/lib/hal-o-swarm/supervisor.db.current.$(date +%s)

# 5. Restore from point-in-time backup
sudo cp /backup/supervisor.db.$BACKUP_DATE \
  /var/lib/hal-o-swarm/supervisor.db

# 6. Verify permissions
sudo chown hal-supervisor:hal-supervisor /var/lib/hal-o-swarm/supervisor.db
sudo chmod 600 /var/lib/hal-o-swarm/supervisor.db

# 7. Start supervisor
sudo systemctl start hal-supervisor

# 8. Verify it started
sudo systemctl status hal-supervisor

# 9. Check logs for errors
sudo journalctl -u hal-supervisor --since "5 minutes ago"
```

### Backup Strategy

Implement automated daily backups:

```bash
# Add to crontab
0 2 * * * cp /var/lib/hal-o-swarm/supervisor.db /backup/supervisor.db.$(date +\%Y\%m\%d)

# Verify backups are being created
ls -lh /backup/supervisor.db.*

# Clean up old backups (keep 30 days)
find /backup -name "supervisor.db.*" -mtime +30 -delete
```

---

## Verification

### Post-Rollback Verification

After completing rollback, verify system is operational:

```bash
# 1. Check supervisor status
sudo systemctl status hal-supervisor

# 2. Check agent status
sudo systemctl status hal-agent

# 3. Verify supervisor is listening
sudo netstat -tlnp | grep 8420

# 4. Check agent connections
halctl --supervisor-url ws://localhost:8420 nodes list

# 5. Verify sessions are accessible
halctl --supervisor-url ws://localhost:8420 sessions list

# 6. Check cost data
halctl --supervisor-url ws://localhost:8420 cost today

# 7. Monitor logs for errors
sudo journalctl -u hal-supervisor -p err --since "1 hour ago"
sudo journalctl -u hal-agent -p err --since "1 hour ago"

# 8. Test API endpoints
curl http://localhost:8421/healthz
curl http://localhost:8421/readyz
```

### Rollback Validation Checklist

- [ ] Supervisor process is running
- [ ] Agent processes are running
- [ ] All agents show "online" status
- [ ] Sessions are accessible via CLI
- [ ] Cost data is present
- [ ] No errors in logs
- [ ] API endpoints respond correctly
- [ ] Discord alerts are working

### Performance Verification

```bash
# Check response times
time halctl --supervisor-url ws://localhost:8420 sessions list

# Monitor resource usage
watch -n 5 'ps aux | grep hal-supervisor'

# Check database performance
sqlite3 /var/lib/hal-o-swarm/supervisor.db ".timer on"
sqlite3 /var/lib/hal-o-swarm/supervisor.db "SELECT COUNT(*) FROM sessions;"
```

---

## Rollback Scenarios

### Scenario 1: Supervisor Update Breaks API

**Issue**: New supervisor version breaks HTTP API

**Rollback Steps**:

```bash
# 1. Identify issue
sudo journalctl -u hal-supervisor | grep -i "error\|panic"

# 2. Rollback binary
sudo systemctl stop hal-supervisor
sudo cp /usr/local/bin/hal-supervisor.backup /usr/local/bin/hal-supervisor
sudo systemctl start hal-supervisor

# 3. Verify API is working
curl http://localhost:8421/healthz

# 4. Notify team
echo "Supervisor rolled back to previous version" > /tmp/incident.txt
```

### Scenario 2: Agent Configuration Breaks Connection

**Issue**: Configuration change prevents agents from connecting

**Rollback Steps**:

```bash
# 1. Identify issue
sudo journalctl -u hal-agent | grep -i "connection\|error"

# 2. Rollback configuration
sudo cp /etc/hal-o-swarm/agent.config.json.backup \
  /etc/hal-o-swarm/agent.config.json

# 3. Restart agent
sudo systemctl restart hal-agent

# 4. Verify connection
sudo journalctl -u hal-agent --since "1 minute ago"
```

### Scenario 3: Database Corruption After Update

**Issue**: Database becomes corrupted after supervisor update

**Rollback Steps**:

```bash
# 1. Stop supervisor
sudo systemctl stop hal-supervisor

# 2. Restore database backup
sudo cp /var/lib/hal-o-swarm/supervisor.db.backup \
  /var/lib/hal-o-swarm/supervisor.db

# 3. Rollback supervisor binary
sudo cp /usr/local/bin/hal-supervisor.backup /usr/local/bin/hal-supervisor

# 4. Start supervisor
sudo systemctl start hal-supervisor

# 5. Verify database integrity
sqlite3 /var/lib/hal-o-swarm/supervisor.db "PRAGMA integrity_check;"
```

---

## Prevention

### Backup Checklist

- [ ] Daily automated backups enabled
- [ ] Backups stored on separate system
- [ ] Backup retention policy enforced (30+ days)
- [ ] Backup restoration tested monthly
- [ ] Backup integrity verified regularly

### Testing Checklist

- [ ] Test rollback procedure monthly
- [ ] Verify backup restoration works
- [ ] Document any issues found
- [ ] Update procedures based on findings

### Monitoring Checklist

- [ ] Monitor backup creation: `ls -lh /backup/supervisor.db.*`
- [ ] Monitor backup size for anomalies
- [ ] Alert on backup failures
- [ ] Alert on database corruption

---

## Related Documentation

- [DEPLOYMENT.md](DEPLOYMENT.md) - Deployment procedures
- [RUNBOOK.md](RUNBOOK.md) - Incident response procedures
- [README.md](../README.md) - Architecture overview
