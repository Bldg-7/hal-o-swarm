# HAL-O-SWARM Incident Response Runbook

**Version 1.0** | February 2026

Operational procedures for responding to common incidents in HAL-O-SWARM deployments.

## Table of Contents

1. [Incident Classification](#incident-classification)
2. [Supervisor Incidents](#supervisor-incidents)
3. [Agent Incidents](#agent-incidents)
4. [Network Incidents](#network-incidents)
5. [Data Incidents](#data-incidents)
6. [Escalation](#escalation)

---

## Incident Classification

| Severity | Impact | Response Time | Examples |
|----------|--------|----------------|----------|
| **Critical** | System down, data loss risk | Immediate (< 5 min) | Supervisor crash, database corruption |
| **High** | Partial outage, degraded performance | Urgent (< 30 min) | Agent disconnected, memory leak |
| **Medium** | Feature unavailable, workaround exists | Standard (< 2 hours) | API endpoint down, policy engine stuck |
| **Low** | Minor issue, no user impact | Routine (< 24 hours) | Log rotation failed, metrics unavailable |

---

## Supervisor Incidents

### Supervisor Process Crashed

**Symptoms**:
- `systemctl status hal-supervisor` shows "inactive"
- Agents report connection refused
- Discord alerts stop appearing

**Diagnosis**:

```bash
# Check service status
sudo systemctl status hal-supervisor

# View crash logs
sudo journalctl -u hal-supervisor -n 50

# Check if process is running
ps aux | grep hal-supervisor

# Check port availability
sudo lsof -i :8420
```

**Resolution**:

```bash
# Restart supervisor
sudo systemctl restart hal-supervisor

# Verify it started
sudo systemctl status hal-supervisor

# Check logs for errors
sudo journalctl -u hal-supervisor --since "5 minutes ago"

# If restart fails, check configuration
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# If config is invalid, restore from backup
sudo cp /etc/hal-o-swarm/supervisor.config.json.backup /etc/hal-o-swarm/supervisor.config.json
sudo systemctl restart hal-supervisor
```

**Prevention**:
- Monitor systemd restart count: `systemctl show hal-supervisor -p NRestarts`
- Set up alerting on repeated restarts (> 3 in 5 minutes)
- Review logs daily for warnings

---

### Supervisor High Memory Usage

**Symptoms**:
- `ps aux | grep hal-supervisor` shows > 2GB memory
- System becomes unresponsive
- OOM killer may terminate process

**Diagnosis**:

```bash
# Check memory usage
ps aux | grep hal-supervisor

# Check memory limit
systemctl show hal-supervisor -p MemoryLimit

# Check for memory leaks
sudo journalctl -u hal-supervisor | grep -i "memory\|leak"

# Check active sessions
halctl --supervisor-url ws://localhost:8420 sessions list | wc -l

# Check database size
du -sh /var/lib/hal-o-swarm/supervisor.db
```

**Resolution**:

```bash
# Immediate: Increase memory limit
sudo systemctl edit hal-supervisor
# Change MemoryLimit=2G to MemoryLimit=4G
sudo systemctl daemon-reload
sudo systemctl restart hal-supervisor

# Short-term: Archive old data
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"

# Long-term: Investigate memory leak
sudo journalctl -u hal-supervisor -p err --since "24 hours ago"
# File bug report with logs
```

**Prevention**:
- Monitor memory usage: `watch -n 5 'ps aux | grep hal-supervisor'`
- Set up alerting on memory > 80% of limit
- Implement automatic data archival (see ROLLBACK.md)

---

### Supervisor Database Corruption

**Symptoms**:
- Supervisor won't start: "database disk image is malformed"
- Queries fail with "database corruption detected"
- Supervisor crashes on startup

**Diagnosis**:

```bash
# Check database integrity
sqlite3 /var/lib/hal-o-swarm/supervisor.db "PRAGMA integrity_check;"

# Check database size
ls -lh /var/lib/hal-o-swarm/supervisor.db

# Check recent errors
sudo journalctl -u hal-supervisor -p err -n 20
```

**Resolution**:

```bash
# Stop supervisor
sudo systemctl stop hal-supervisor

# Backup corrupted database
sudo cp /var/lib/hal-o-swarm/supervisor.db \
  /var/lib/hal-o-swarm/supervisor.db.corrupt.$(date +%s)

# Attempt recovery
sqlite3 /var/lib/hal-o-swarm/supervisor.db ".recover" | \
  sqlite3 /var/lib/hal-o-swarm/supervisor.db.recovered

# If recovery succeeds, use recovered database
sudo mv /var/lib/hal-o-swarm/supervisor.db.recovered \
  /var/lib/hal-o-swarm/supervisor.db

# If recovery fails, restore from backup
sudo cp /var/lib/hal-o-swarm/supervisor.db.backup \
  /var/lib/hal-o-swarm/supervisor.db

# Restart supervisor
sudo systemctl start hal-supervisor

# Verify
sudo systemctl status hal-supervisor
```

**Prevention**:
- Enable WAL mode: `sqlite3 /var/lib/hal-o-swarm/supervisor.db "PRAGMA journal_mode=WAL;"`
- Regular backups: `0 2 * * * cp /var/lib/hal-o-swarm/supervisor.db /backup/supervisor.db.$(date +\%Y\%m\%d)`
- Monitor disk space to prevent corruption from full disk

---

## Agent Incidents

### Agent Can't Connect to Supervisor

**Symptoms**:
- Agent logs show "connection refused" or "timeout"
- Agent status shows "offline"
- No events from this agent in supervisor

**Diagnosis**:

```bash
# Check agent status
sudo systemctl status hal-agent

# Check agent logs
sudo journalctl -u hal-agent -n 50

# Test connectivity
curl -v ws://supervisor-host:8420

# Check supervisor is running
ssh supervisor-host "sudo systemctl status hal-supervisor"

# Check firewall
sudo ufw status | grep 8420
sudo iptables -L -n | grep 8420

# Check auth token
grep auth_token /etc/hal-o-swarm/agent.config.json
ssh supervisor-host "grep auth_token /etc/hal-o-swarm/supervisor.config.json"
```

**Resolution**:

```bash
# Verify supervisor is running
ssh supervisor-host "sudo systemctl status hal-supervisor"

# If supervisor is down, restart it
ssh supervisor-host "sudo systemctl restart hal-supervisor"

# Verify auth tokens match
sudo nano /etc/hal-o-swarm/agent.config.json
ssh supervisor-host "sudo nano /etc/hal-o-swarm/supervisor.config.json"

# Check firewall rules
sudo ufw allow 8420/tcp

# Restart agent
sudo systemctl restart hal-agent

# Verify connection
sudo journalctl -u hal-agent --since "1 minute ago"
```

**Prevention**:
- Monitor agent connection status: `halctl nodes list`
- Set up alerting on agent offline (> 2 minutes)
- Document supervisor hostname/IP in agent config comments

---

### Agent High Memory Usage

**Symptoms**:
- `ps aux | grep hal-agent` shows > 4GB memory
- Agent becomes unresponsive
- OOM killer may terminate process

**Diagnosis**:

```bash
# Check memory usage
ps aux | grep hal-agent

# Check memory limit
systemctl show hal-agent -p MemoryLimit

# Check active sessions
halctl --supervisor-url ws://localhost:8420 sessions list | grep "node_id: <agent-id>"

# Check for memory leaks
sudo journalctl -u hal-agent | grep -i "memory\|leak"
```

**Resolution**:

```bash
# Immediate: Increase memory limit
sudo systemctl edit hal-agent
# Change MemoryLimit=4G to MemoryLimit=8G
sudo systemctl daemon-reload
sudo systemctl restart hal-agent

# Short-term: Kill idle sessions
halctl --supervisor-url ws://localhost:8420 sessions list --node-id <agent-id>
halctl --supervisor-url ws://localhost:8420 sessions kill <session-id>

# Long-term: Investigate memory leak
sudo journalctl -u hal-agent -p err --since "24 hours ago"
```

**Prevention**:
- Monitor memory usage: `watch -n 5 'ps aux | grep hal-agent'`
- Set up alerting on memory > 80% of limit
- Implement session timeout policy

---

### Agent Process Crashed

**Symptoms**:
- `systemctl status hal-agent` shows "inactive"
- Supervisor shows agent as "offline"
- No new events from this agent

**Diagnosis**:

```bash
# Check service status
sudo systemctl status hal-agent

# View crash logs
sudo journalctl -u hal-agent -n 50

# Check if process is running
ps aux | grep hal-agent

# Check configuration
/usr/local/bin/hal-agent --config /etc/hal-o-swarm/agent.config.json --validate
```

**Resolution**:

```bash
# Restart agent
sudo systemctl restart hal-agent

# Verify it started
sudo systemctl status hal-agent

# Check logs for errors
sudo journalctl -u hal-agent --since "5 minutes ago"

# If restart fails, check configuration
/usr/local/bin/hal-agent --config /etc/hal-o-swarm/agent.config.json --validate

# If config is invalid, restore from backup
sudo cp /etc/hal-o-swarm/agent.config.json.backup /etc/hal-o-swarm/agent.config.json
sudo systemctl restart hal-agent
```

**Prevention**:
- Monitor systemd restart count: `systemctl show hal-agent -p NRestarts`
- Set up alerting on repeated restarts (> 3 in 5 minutes)
- Review logs daily for warnings

---

## Network Incidents

### Supervisor Unreachable from Agents

**Symptoms**:
- All agents show "offline"
- Network connectivity appears normal
- Supervisor process is running

**Diagnosis**:

```bash
# Check supervisor is listening
sudo netstat -tlnp | grep 8420

# Check firewall rules
sudo ufw status
sudo iptables -L -n | grep 8420

# Test connectivity from agent
ssh agent-host "curl -v ws://supervisor-host:8420"

# Check DNS resolution
nslookup supervisor-host
ssh agent-host "nslookup supervisor-host"

# Check network routing
traceroute supervisor-host
ssh agent-host "traceroute supervisor-host"
```

**Resolution**:

```bash
# Verify supervisor is listening
sudo netstat -tlnp | grep 8420

# If not listening, restart supervisor
sudo systemctl restart hal-supervisor

# Check firewall rules
sudo ufw allow 8420/tcp

# If using iptables
sudo iptables -A INPUT -p tcp --dport 8420 -j ACCEPT
sudo iptables-save > /etc/iptables/rules.v4

# Test connectivity
ssh agent-host "curl -v ws://supervisor-host:8420"

# Restart agents
ssh agent-host "sudo systemctl restart hal-agent"
```

**Prevention**:
- Document network topology and firewall rules
- Test connectivity regularly: `watch -n 60 'curl -s ws://supervisor-host:8420 || echo FAILED'`
- Set up alerting on connection failures

---

### High Latency / Slow Responses

**Symptoms**:
- Commands take > 10 seconds to execute
- Supervisor CPU usage is high
- Network latency appears normal

**Diagnosis**:

```bash
# Check supervisor CPU usage
top -p $(pgrep -f hal-supervisor)

# Check network latency
ping supervisor-host

# Check supervisor load
uptime

# Check database query performance
sqlite3 /var/lib/hal-o-swarm/supervisor.db ".timer on"
sqlite3 /var/lib/hal-o-swarm/supervisor.db "SELECT COUNT(*) FROM sessions;"

# Check active connections
ss -tnp | grep hal-supervisor | wc -l
```

**Resolution**:

```bash
# Immediate: Restart supervisor
sudo systemctl restart hal-supervisor

# Short-term: Optimize database
sqlite3 /var/lib/hal-o-swarm/supervisor.db "VACUUM;"
sqlite3 /var/lib/hal-o-swarm/supervisor.db "ANALYZE;"

# Long-term: Archive old data
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"

# Monitor performance
watch -n 5 'top -p $(pgrep -f hal-supervisor) -b -n 1'
```

**Prevention**:
- Monitor query performance: `sqlite3 /var/lib/hal-o-swarm/supervisor.db ".timer on"`
- Set up alerting on response time > 5 seconds
- Implement automatic data archival

---

## Data Incidents

### Lost Session Data

**Symptoms**:
- Session history is missing
- Cost data is incomplete
- Events are not recorded

**Diagnosis**:

```bash
# Check database integrity
sqlite3 /var/lib/hal-o-swarm/supervisor.db "PRAGMA integrity_check;"

# Check recent backups
ls -lh /var/lib/hal-o-swarm/supervisor.db.backup*

# Check if data was deleted
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "SELECT COUNT(*) FROM sessions WHERE timestamp > datetime('now', '-1 day');"
```

**Resolution**:

```bash
# Stop supervisor
sudo systemctl stop hal-supervisor

# Restore from backup
sudo cp /var/lib/hal-o-swarm/supervisor.db.backup \
  /var/lib/hal-o-swarm/supervisor.db

# Restart supervisor
sudo systemctl start hal-supervisor

# Verify data is restored
halctl --supervisor-url ws://localhost:8420 sessions list
```

**Prevention**:
- Enable automated backups: `0 2 * * * cp /var/lib/hal-o-swarm/supervisor.db /backup/supervisor.db.$(date +\%Y\%m\%d)`
- Test backup restoration monthly
- Monitor backup file size for anomalies

---

### Cost Data Discrepancy

**Symptoms**:
- Cost reports don't match provider dashboards
- Cost data is missing for certain dates
- Cost aggregator is not updating

**Diagnosis**:

```bash
# Check cost aggregator status
sudo journalctl -u hal-supervisor | grep -i "cost"

# Check cost data in database
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "SELECT * FROM costs ORDER BY date DESC LIMIT 10;"

# Check provider API connectivity
curl -H "Authorization: Bearer <api-key>" \
  https://api.anthropic.com/v1/usage

# Check cost configuration
grep -A 10 '"cost"' /etc/hal-o-swarm/supervisor.config.json
```

**Resolution**:

```bash
# Verify provider API keys are correct
sudo nano /etc/hal-o-swarm/supervisor.config.json

# Restart cost aggregator
sudo systemctl restart hal-supervisor

# Force cost update
halctl --supervisor-url ws://localhost:8420 cost today

# Check for errors
sudo journalctl -u hal-supervisor -p err --since "1 hour ago"
```

**Prevention**:
- Monitor cost data: `halctl cost today`
- Set up alerting on cost update failures
- Verify provider API keys monthly

---

## Escalation

### When to Escalate

Escalate to senior operations team if:
- Issue persists after following runbook procedures
- Multiple components are affected
- Data loss has occurred
- Security incident is suspected

### Escalation Procedure

1. **Document the incident**:
   ```bash
   # Collect logs
   sudo journalctl -u hal-supervisor -n 1000 > /tmp/supervisor.log
   sudo journalctl -u hal-agent -n 1000 > /tmp/agent.log
   
   # Collect system info
   uname -a > /tmp/system.info
   df -h >> /tmp/system.info
   free -h >> /tmp/system.info
   ```

2. **Create incident ticket** with:
   - Incident classification (Critical/High/Medium/Low)
   - Timeline of events
   - Symptoms observed
   - Diagnosis performed
   - Resolution attempted
   - Attached logs and system info

3. **Notify stakeholders**:
   - Post in #incidents Discord channel
   - Email ops-team@example.com
   - Page on-call engineer if Critical

4. **Maintain communication**:
   - Update ticket every 15 minutes
   - Post status updates to Discord
   - Notify when resolved

---

## Contact Information

- **On-Call Engineer**: See PagerDuty schedule
- **Ops Team**: ops-team@example.com
- **Discord**: #incidents channel
- **Escalation**: escalation@example.com

---

## Related Documentation

- [DEPLOYMENT.md](DEPLOYMENT.md) - Deployment procedures
- [ROLLBACK.md](ROLLBACK.md) - Rollback procedures
- [README.md](../README.md) - Architecture overview
