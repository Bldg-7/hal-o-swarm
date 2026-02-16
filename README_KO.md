# HAL-O-SWARM

**분산 LLM 에이전트 슈퍼바이저** — 여러 서버에서 실행되는 자율 LLM 코딩 에이전트를 위한 실시간 모니터링 및 제어 플레인

![Version](https://img.shields.io/badge/version-1.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Status](https://img.shields.io/badge/status-production-brightgreen)

## 개요

HAL-O-SWARM은 다음 기능을 제공하는 분산 슈퍼바이저 데몬입니다:

- **실시간 모니터링**: 여러 서버에서 실행되는 LLM 에이전트 세션 추적
- **통합 제어 플레인**: Discord/Slack 명령을 통한 원격 개입
- **비용 추적**: Anthropic, OpenAI 등 여러 제공업체의 LLM API 비용 집계
- **자동 개입**: 자동 세션 복구, 재시작 및 비용 관리 정책
- **이벤트 스트리밍**: 필터링 및 라우팅이 가능한 실시간 이벤트 파이프라인
- **감사 로깅**: 규정 준수 및 디버깅을 위한 완전한 감사 추적

### 아키텍처

```
┌─────────────────────────────────────────┐
│    Discord / Slack (사용자 인터페이스)    │
│  명령 입력, 알림 출력, 비용 보고서        │
└──────────────────┬──────────────────────┘
                   │ Webhook / Bot API
┌──────────────────▼──────────────────────┐
│   hal-supervisor (중앙 데몬)             │
│  - 세션 추적                             │
│  - 이벤트 라우팅                         │
│  - 비용 집계                             │
│  - 명령 디스패치                         │
│  - 정책 엔진                             │
└──────────────────┬──────────────────────┘
                   │ WebSocket (아웃바운드)
        ┌──────────┴──────────┐
        ▼                     ▼
┌──────────────────┐  ┌──────────────────┐
│  hal-agent       │  │  hal-agent       │
│  (노드 A)        │  │  (노드 B)        │
│  opencode serve  │  │  opencode serve  │
│  [P1] [P3] [P4]  │  │  [P6] [P7]       │
└──────────────────┘  └──────────────────┘
```

## 빠른 시작

### 설치

```bash
# 저장소 클론
git clone https://github.com/code-yeongyu/hal-o-swarm.git
cd hal-o-swarm

# 모든 컴포넌트 설치
sudo ./deploy/install.sh --all

# 설정 편집
sudo nano /etc/hal-o-swarm/supervisor.config.json
sudo nano /etc/hal-o-swarm/agent.config.json

# 서비스 시작
sudo systemctl start hal-supervisor
sudo systemctl start hal-agent

# 부팅 시 자동 시작 활성화
sudo systemctl enable hal-supervisor
sudo systemctl enable hal-agent
```

### 설치 확인

```bash
# 슈퍼바이저 상태 확인
sudo systemctl status hal-supervisor

# 에이전트 상태 확인
sudo systemctl status hal-agent

# 노드 목록 조회
halctl --supervisor-url ws://localhost:8420 --auth-token <토큰> nodes list

# 세션 목록 조회
halctl --supervisor-url ws://localhost:8420 --auth-token <토큰> sessions list
```

## 문서

- **[DEPLOYMENT.md](docs/DEPLOYMENT.md)** - 설정 옵션이 포함된 완전한 배포 가이드
- **[RUNBOOK.md](docs/RUNBOOK.md)** - 일반적인 문제에 대한 인시던트 대응 절차
- **[ROLLBACK.md](docs/ROLLBACK.md)** - 안전한 롤백 및 복구 절차
- **[제품 사양](Hal-o-swarm_Product_Spec_v1.1.md)** - 상세한 시스템 사양

## 기능

### 세션 관리

- LLM 에이전트 세션 생성, 모니터링 및 제어
- 세션 상태 추적: running, idle, compacted, error, completed
- 세션 로그 및 이벤트 히스토리 조회
- 원격 세션 개입 (재시작, 종료, 프롬프트 주입)

### 이벤트 파이프라인

- 에이전트로부터의 실시간 이벤트 스트리밍
- 이벤트 필터링 및 라우팅
- SQLite를 사용한 이벤트 영속성
- 이벤트 재생 및 감사 추적

### 비용 추적

- 여러 LLM 제공업체의 비용 집계
- 제공업체 및 모델별 일일 비용 버킷팅
- 프로젝트 수준 비용 가시성
- 비용 기반 자동 개입 정책

### 자동 개입 정책

- **유휴 시 재개**: 유휴 세션 자동 재개
- **압축 시 재시작**: 컨텍스트 윈도우가 가득 찰 때 세션 재시작
- **비용 초과 시 종료**: 비용 임계값을 초과하는 세션 종료
- 구성 가능한 재시도 제한 및 재설정 윈도우

### Discord 통합

- 세션 관리를 위한 슬래시 명령
- 실시간 알림 및 알림
- 비용 보고서 및 상태 쿼리
- 명령 감사 로깅

### HTTP API

- 프로그래밍 방식 액세스를 위한 RESTful API
- Bearer 토큰 인증
- JSON 응답 엔벨로프
- 포괄적인 오류 처리

### CLI 도구 (halctl)

```bash
# 노드 목록 조회
halctl nodes list

# 노드 상세 정보 조회
halctl nodes get <노드-ID>

# 세션 목록 조회
halctl sessions list

# 세션 상세 정보 조회
halctl sessions get <세션-ID>

# 세션 로그 조회
halctl sessions logs <세션-ID>

# 비용 보고서 조회
halctl cost today
halctl cost week
halctl cost month

# 환경 확인
halctl env status <프로젝트>

# 환경 프로비저닝
halctl env provision <프로젝트>
```

## 설정

### 슈퍼바이저 설정

```json
{
  "server": {
    "port": 8420,
    "http_port": 8421,
    "auth_token": "여기에-공유-비밀-입력",
    "heartbeat_interval_sec": 30,
    "heartbeat_timeout_count": 3
  },
  "channels": {
    "discord": {
      "bot_token": "디스코드-봇-토큰",
      "guild_id": "길드-ID",
      "channels": {
        "alerts": "알림용-채널-ID",
        "dev-log": "개발-로그용-채널-ID"
      }
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": "Anthropic-관리자-API-키"
      }
    }
  },
  "policies": {
    "resume_on_idle": {
      "enabled": true,
      "idle_threshold_seconds": 300,
      "max_retries": 3
    }
  }
}
```

### 에이전트 설정

```json
{
  "supervisor_url": "ws://슈퍼바이저-호스트:8420",
  "auth_token": "여기에-공유-비밀-입력",
  "opencode_port": 4096,
  "projects": [
    {
      "name": "프로젝트-1",
      "directory": "/home/user/프로젝트-1"
    }
  ]
}
```

## 개발

### 프로젝트 구조

```
hal-o-swarm/
├── cmd/
│   ├── supervisor/      # 슈퍼바이저 진입점
│   ├── agent/           # 에이전트 진입점
│   └── halctl/          # CLI 도구 진입점
├── internal/
│   ├── supervisor/      # 슈퍼바이저 구현
│   │   ├── registry.go  # 노드 레지스트리
│   │   ├── tracker.go   # 세션 트래커
│   │   ├── router.go    # 이벤트 라우터
│   │   ├── cost.go      # 비용 집계기
│   │   ├── commands.go  # 명령 디스패처
│   │   └── policy.go    # 정책 엔진
│   ├── agent/           # 에이전트 구현
│   │   ├── proxy.go     # 세션 프록시
│   │   ├── wsclient.go  # WebSocket 클라이언트
│   │   └── forwarder.go # 이벤트 포워더
│   ├── halctl/          # CLI 구현
│   ├── shared/          # 공유 타입 및 프로토콜
│   └── config/          # 설정 검증
├── deploy/
│   ├── systemd/         # Systemd 유닛
│   ├── install.sh       # 설치 스크립트
│   └── uninstall.sh     # 제거 스크립트
├── docs/
│   ├── DEPLOYMENT.md    # 배포 가이드
│   ├── RUNBOOK.md       # 인시던트 대응
│   └── ROLLBACK.md      # 롤백 절차
└── integration/         # 통합 테스트
```

### 소스에서 빌드

```bash
# 슈퍼바이저 빌드
go build -o supervisor ./cmd/supervisor

# 에이전트 빌드
go build -o agent ./cmd/agent

# CLI 도구 빌드
go build -o halctl ./cmd/halctl

# 테스트 실행
go test -race ./...

# 커버리지와 함께 실행
go test -race -cover ./...
```

### 테스트 실행

```bash
# 모든 테스트 실행
go test -race ./...

# 특정 패키지 테스트 실행
go test -race ./internal/supervisor/...

# 상세 출력과 함께 실행
go test -race -v ./...

# 통합 테스트 실행
go test -race ./integration/...
```

## 모니터링

### 헬스 체크

```bash
# 라이브니스 프로브 (실행 중이면 항상 정상)
curl http://localhost:8421/healthz

# 레디니스 프로브 (모든 컴포넌트 확인)
curl http://localhost:8421/readyz

# Prometheus 메트릭
curl http://localhost:8421/metrics
```

### Systemd 모니터링

```bash
# 서비스 상태 확인
sudo systemctl status hal-supervisor

# 로그 조회
sudo journalctl -u hal-supervisor -f

# 재시작 횟수 확인
sudo systemctl show hal-supervisor -p NRestarts
```

### 주요 메트릭

- `hal_o_swarm_commands_total` - 실행된 총 명령 수
- `hal_o_swarm_events_total` - 처리된 총 이벤트 수
- `hal_o_swarm_connections_active` - 현재 활성 연결 수
- `hal_o_swarm_sessions_active` - 상태별 현재 세션 수
- `hal_o_swarm_nodes_online` - 현재 온라인 노드 수
- `hal_o_swarm_command_duration_seconds` - 명령 실행 시간

## 문제 해결

### 슈퍼바이저가 시작되지 않음

```bash
# 설정 확인
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# 로그 조회
sudo journalctl -u hal-supervisor -n 50

# 포트 사용 가능 여부 확인
sudo lsof -i :8420
```

### 에이전트가 연결할 수 없음

```bash
# 슈퍼바이저 실행 중인지 확인
sudo systemctl status hal-supervisor

# 인증 토큰 일치 확인
grep auth_token /etc/hal-o-swarm/supervisor.config.json
grep auth_token /etc/hal-o-swarm/agent.config.json

# 연결 테스트
curl -v ws://슈퍼바이저-호스트:8420

# 에이전트 로그 확인
sudo journalctl -u hal-agent -n 50
```

### 높은 메모리 사용량

```bash
# 메모리 사용량 확인
ps aux | grep hal-supervisor

# 메모리 제한 증가
sudo systemctl edit hal-supervisor
# MemoryLimit=2G를 MemoryLimit=4G로 변경

# 오래된 데이터 아카이브
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"
```

더 많은 문제 해결 절차는 [RUNBOOK.md](docs/RUNBOOK.md)를 참조하세요.

## 보안

### TLS 설정

프로덕션 배포를 위한 TLS 활성화:

```json
{
  "security": {
    "tls": {
      "enabled": true,
      "cert_path": "/etc/hal-o-swarm/cert.pem",
      "key_path": "/etc/hal-o-swarm/key.pem"
    }
  }
}
```

### Origin 허용 목록

알려진 origin으로 WebSocket 연결 제한:

```json
{
  "security": {
    "origin_allowlist": [
      "http://localhost:*",
      "https://internal.example.com:*"
    ]
  }
}
```

### 감사 로깅

규정 준수를 위한 감사 로깅 활성화:

```json
{
  "security": {
    "audit": {
      "enabled": true,
      "retention_days": 90
    }
  }
}
```

## 성능

### 권장 하드웨어

| 컴포넌트 | CPU | 메모리 | 디스크 | 네트워크 |
|----------|-----|--------|--------|----------|
| 슈퍼바이저 | 2+ 코어 | 2GB+ | 10GB+ | 100Mbps+ |
| 에이전트 | 2+ 코어 | 4GB+ | 20GB+ | 100Mbps+ |

### 최적화 팁

- 오래된 이벤트 정기적으로 아카이브: `DELETE FROM events WHERE timestamp < datetime('now', '-30 days')`
- 데이터베이스 정리: `VACUUM;`
- 데이터베이스 분석: `ANALYZE;`
- 쿼리 성능 모니터링: `sqlite3 /var/lib/hal-o-swarm/supervisor.db ".timer on"`

## 기여

기여를 환영합니다! 다음 절차를 따라주세요:

1. 저장소 포크
2. 기능 브랜치 생성
3. 변경 사항 작성
4. 테스트 추가
5. Pull Request 제출

## 라이선스

MIT 라이선스 - 자세한 내용은 LICENSE 파일 참조

## 지원

- **문서**: [docs/](docs/) 디렉토리 참조
- **이슈**: GitHub Issues
- **Discord**: #hal-o-swarm 채널
- **이메일**: support@example.com

## 변경 로그

### 버전 1.0.0 (2026년 2월)

- 초기 릴리스
- 세션 추적 및 이벤트 라우팅이 포함된 슈퍼바이저
- WebSocket 재연결 및 이벤트 포워딩이 포함된 에이전트
- Discord 슬래시 명령 통합
- Bearer 토큰 인증이 포함된 HTTP API
- Anthropic 및 OpenAI의 비용 집계
- 자동 개입 정책 엔진
- 원격 관리를 위한 CLI 도구 (halctl)
- 포괄적인 배포 및 런북 문서

## 로드맵

- [ ] Slack 통합
- [ ] Kubernetes 배포 지원
- [ ] 다중 리전 페더레이션
- [ ] 고급 분석 및 보고
- [ ] 사용자 정의 정책 스크립팅
- [ ] 웹 UI 대시보드

---

**HAL-O-SWARM 팀이 ❤️로 만들었습니다**
