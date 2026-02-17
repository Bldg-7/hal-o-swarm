# HAL-O-SWARM

**분산 LLM 에이전트 슈퍼바이저** — 여러 서버에서 실행되는 자율 LLM 코딩 에이전트를 위한 실시간 모니터링 및 제어 플레인

![Version](https://img.shields.io/badge/version-1.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Status](https://img.shields.io/badge/status-production-brightgreen)

## 개요

HAL-O-SWARM은 분산된 LLM 코딩 에이전트에 대한 중앙 집중식 감독을 제공하여 인프라 전체에서 실시간 모니터링, 원격 제어, 비용 추적 및 자동 개입을 가능하게 합니다.

**주요 기능:**
- 🔍 **실시간 모니터링** — 모든 노드에서 에이전트 세션, 이벤트 및 리소스 사용량 추적
- 🎮 **통합 제어** — Discord 명령, HTTP API 또는 CLI를 통한 세션 관리
- 💰 **비용 추적** — 여러 제공업체의 LLM API 비용 집계 및 분석
- 🤖 **자동 개입** — 자동 세션 복구, 재시작 및 비용 관리
- 📊 **이벤트 스트리밍** — 필터링 및 영속성을 갖춘 실시간 이벤트 파이프라인
- 🔒 **보안 및 감사** — TLS 지원, origin 검증 및 완전한 감사 추적

---

## 아키텍처

### 시스템 개요

```
┌─────────────────────────────────────────────────────────────┐
│                      제어 인터페이스                          │
│  Discord Bot  │  HTTP API  │  CLI (halctl)  │  Prometheus   │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                  hal-supervisor (중앙 허브)                  │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   세션       │  │   이벤트     │  │   명령       │      │
│  │   트래커     │  │  파이프라인  │  │  디스패처    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │    비용      │  │    정책      │  │    노드      │      │
│  │   집계기     │  │    엔진      │  │  레지스트리  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                               │
│  저장소: SQLite (세션, 이벤트, 비용, 감사)                   │
└────────────────────────┬────────────────────────────────────┘
                         │ WebSocket (양방향)
          ┌──────────────┼──────────────┐
          │              │              │
┌─────────▼─────────┐ ┌─▼──────────────▼┐ ┌─────────────────┐
│   hal-agent       │ │   hal-agent     │ │   hal-agent     │
│   (노드 A)        │ │   (노드 B)      │ │   (노드 C)      │
│                   │ │                 │ │                 │
│  opencode serve   │ │  opencode serve │ │  opencode serve │
│  ├─ 프로젝트-1    │ │  ├─ 프로젝트-3  │ │  ├─ 프로젝트-5  │
│  ├─ 프로젝트-2    │ │  └─ 프로젝트-4  │ │  └─ 프로젝트-6  │
│  └─ 프로젝트-3    │ │                 │ │                 │
└───────────────────┘ └─────────────────┘ └─────────────────┘
```

### 컴포넌트 역할

#### Supervisor (중앙 허브)
- **세션 트래커**: 노드 전체의 모든 에이전트 세션 상태 유지
- **이벤트 파이프라인**: 중복 제거를 통한 이벤트 처리, 정렬 및 영속화
- **명령 디스패처**: 멱등성 보장과 함께 에이전트로 명령 라우팅
- **비용 집계기**: LLM 제공업체 API 폴링 및 사용량 데이터 집계
- **정책 엔진**: 자동 개입 규칙 실행 (재개, 재시작, 종료)
- **노드 레지스트리**: 하트비트 모니터링을 통한 에이전트 상태 추적

#### Agent (노드 프로세스)
- **WebSocket 클라이언트**: 자동 재연결을 통한 슈퍼바이저와의 지속적 연결 유지
- **opencode 어댑터**: 세션 관리를 위한 opencode SDK 래핑
- **이벤트 포워더**: 실시간으로 슈퍼바이저에 세션 이벤트 스트리밍
- **환경 체커**: 런타임, 도구 및 프로젝트 요구사항 검증
- **자동 프로비저너**: 누락된 파일 및 설정을 안전하게 생성

#### CLI 도구 (halctl)
- 원격 세션 관리
- 노드 상태 조회
- 비용 보고
- 환경 검증

---

## 빠른 시작

### 설치

```bash
# 저장소 클론
git clone https://github.com/bldg-7/hal-o-swarm.git
cd hal-o-swarm

# 모든 컴포넌트 설치 (릴리즈 바이너리 다운로드, 로컬 빌드 없음)
sudo ./deploy/install-release.sh --all

# 또는 개별 설치
sudo ./deploy/install-release.sh --supervisor  # 중앙 허브만
sudo ./deploy/install-release.sh --agent       # 에이전트만
sudo ./deploy/install-release.sh --halctl      # CLI 도구만
```

### 설정

#### Supervisor 설정 (`/etc/hal-o-swarm/supervisor.config.json`)

```json
{
  "server": {
    "port": 8420,
    "http_port": 8421,
    "auth_token": "여기에-공유-비밀-토큰-입력"
  },
  "database": {
    "path": "/var/lib/hal-o-swarm/supervisor.db"
  },
  "channels": {
    "discord": {
      "bot_token": "디스코드-봇-토큰",
      "guild_id": "길드-ID"
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": "sk-ant-..."
      },
      "openai": {
        "org_api_key": "sk-..."
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

#### Agent 설정 (`/etc/hal-o-swarm/agent.config.json`)

```json
{
  "supervisor_url": "ws://슈퍼바이저-호스트:8420",
  "auth_token": "여기에-공유-비밀-토큰-입력",
  "opencode_port": 4096,
  "projects": [
    {
      "name": "내-프로젝트",
      "directory": "/home/user/내-프로젝트"
    }
  ]
}
```

### 서비스 시작

```bash
# Supervisor 시작
sudo systemctl start hal-supervisor
sudo systemctl enable hal-supervisor

# 각 노드에서 Agent 시작
sudo systemctl start hal-agent
sudo systemctl enable hal-agent

# 상태 확인
sudo systemctl status hal-supervisor
sudo systemctl status hal-agent
```

### 설치 확인

```bash
# 노드 연결 확인
halctl nodes list

# 세션 확인
halctl sessions list

# 헬스 체크
curl http://localhost:8421/healthz
curl http://localhost:8421/readyz
```

---

## 사용법

### Discord 명령

설정 완료 후 Discord에서 다음 슬래시 명령 사용:

```
/status <프로젝트>          # 세션 상태 조회
/nodes                     # 연결된 모든 노드 목록
/logs <세션-ID>            # 세션 로그 조회
/resume <프로젝트>         # 유휴 세션 재개
/restart <세션-ID>         # 세션 재시작
/kill <세션-ID>            # 세션 종료
/start <프로젝트>          # 새 세션 생성
/cost [today|week|month]   # 비용 보고서 조회
```

### CLI 도구 (halctl)

```bash
# 세션 관리
halctl sessions list
halctl sessions get <세션-ID>
halctl sessions logs <세션-ID> --limit 100

# 노드 관리
halctl nodes list
halctl nodes get <노드-ID>

# 비용 보고
halctl cost today
halctl cost week
halctl cost month

# 환경 관리
halctl env status <프로젝트>
halctl env check <프로젝트>
halctl env provision <프로젝트>
```

### HTTP API

```bash
# 세션 목록 조회
curl -H "Authorization: Bearer <토큰>" \
  http://localhost:8421/api/v1/sessions

# 세션 상세 정보 조회
curl -H "Authorization: Bearer <토큰>" \
  http://localhost:8421/api/v1/sessions/<세션-ID>

# 노드 목록 조회
curl -H "Authorization: Bearer <토큰>" \
  http://localhost:8421/api/v1/nodes

# 명령 실행
curl -X POST -H "Authorization: Bearer <토큰>" \
  -H "Content-Type: application/json" \
  -d '{"type":"restart_session","target":"<세션-ID>"}' \
  http://localhost:8421/api/v1/commands

# 비용 보고서 조회
curl -H "Authorization: Bearer <토큰>" \
  http://localhost:8421/api/v1/cost?period=week
```

---

## 기능

### 세션 관리

**인프라 전체에서 LLM 에이전트 세션 추적 및 제어:**

- **실시간 상태 추적**: 세션 상태 모니터링 (running, idle, error, completed)
- **원격 개입**: 실행 중인 세션 재시작, 종료 또는 프롬프트 주입
- **이벤트 히스토리**: 모든 세션 이벤트의 완전한 감사 추적
- **다중 프로젝트 지원**: 각 에이전트가 여러 프로젝트를 동시에 관리 가능

### 이벤트 파이프라인

**순서 보장을 통한 안정적인 이벤트 스트리밍:**

- **시퀀스 추적**: 에이전트별 단조 증가 시퀀스 번호로 이벤트 손실 방지
- **중복 제거**: LRU 캐시로 중복 이벤트 처리 방지
- **갭 감지**: 누락된 이벤트 자동 감지 및 재생
- **영속성**: 효율적인 인덱싱을 통한 SQLite 저장소

### 비용 추적

**LLM 제공업체 전반의 포괄적인 비용 가시성:**

- **다중 제공업체 지원**: Anthropic, OpenAI 및 기타 확장 가능
- **일일 버킷팅**: 날짜, 제공업체 및 모델별 비용 집계
- **프로젝트 귀속**: 차지백을 위한 프로젝트별 비용 추적
- **추세 분석**: 예산 및 예측을 위한 과거 비용 데이터

### 자동 개입 정책

**구성 가능한 규칙 기반 자동 세션 관리:**

- **유휴 시 재개**: 임계값을 초과한 유휴 세션 자동 재개
- **압축 시 재시작**: 컨텍스트 윈도우가 가득 찰 때 세션 재시작
- **비용 초과 시 종료**: 비용 한도를 초과하는 세션 종료
- **재시도 제한**: 무한 루프 방지를 위한 구성 가능한 재시도 상한
- **재설정 윈도우**: 일시적 실패를 위한 시간 기반 재시도 카운터 재설정

### 보안

**프로덕션급 보안 기능:**

- **TLS 지원**: WebSocket 연결을 위한 선택적 WSS 암호화
- **Origin 검증**: 허용 목록 기반 origin 확인
- **토큰 인증**: 모든 연결에 대한 공유 비밀 인증
- **감사 로깅**: 행위자 추적을 통한 완전한 명령 감사 추적
- **비밀 정보 제거**: 로그에서 비밀 정보 자동 삭제

### 관찰성

**내장 모니터링 및 진단:**

- **Prometheus 메트릭**: 모든 작업에 대한 카운터, 게이지 및 히스토그램
- **헬스 엔드포인트**: 오케스트레이션을 위한 라이브니스 및 레디니스 프로브
- **구조화된 로깅**: 상관 ID가 포함된 JSON 로그
- **상관 추적**: 컴포넌트 간 요청 추적

---

## 모니터링

### 헬스 체크

```bash
# 라이브니스 프로브 (실행 중이면 항상 200 반환)
curl http://localhost:8421/healthz

# 레디니스 프로브 (모든 컴포넌트 확인)
curl http://localhost:8421/readyz
# 반환: {"status":"healthy","components":{"database":"ok",...}}
```

### Prometheus 메트릭

```bash
# 메트릭 엔드포인트 스크랩
curl http://localhost:8421/metrics

# 주요 메트릭:
# - hal_o_swarm_commands_total{type,status}
# - hal_o_swarm_events_total{type}
# - hal_o_swarm_connections_active
# - hal_o_swarm_sessions_active{status}
# - hal_o_swarm_nodes_online
# - hal_o_swarm_command_duration_seconds{type}
```

### 로그

```bash
# Supervisor 로그
sudo journalctl -u hal-supervisor -f

# Agent 로그
sudo journalctl -u hal-agent -f

# 상관 ID로 필터링
sudo journalctl -u hal-supervisor | grep "correlation_id=abc123"
```

---

## 설정 참조

### Supervisor 설정

| 섹션 | 키 | 설명 | 기본값 |
|------|-----|------|--------|
| `server` | `port` | WebSocket 서버 포트 | 8420 |
| `server` | `http_port` | HTTP API 포트 | 8421 |
| `server` | `auth_token` | 인증용 공유 비밀 | (필수) |
| `server` | `heartbeat_interval_sec` | 하트비트 간격 | 30 |
| `server` | `heartbeat_timeout_count` | 오프라인 전 누락 하트비트 수 | 3 |
| `database` | `path` | SQLite 데이터베이스 경로 | `/var/lib/hal-o-swarm/supervisor.db` |
| `security.tls` | `enabled` | TLS/WSS 활성화 | false |
| `security.tls` | `cert_path` | TLS 인증서 경로 | - |
| `security.tls` | `key_path` | TLS 개인 키 경로 | - |
| `security` | `origin_allowlist` | 허용된 WebSocket origin | `["*"]` |
| `policies.resume_on_idle` | `enabled` | 자동 재개 활성화 | false |
| `policies.resume_on_idle` | `idle_threshold_seconds` | 유휴 임계값 | 300 |
| `policies.resume_on_idle` | `max_retries` | 최대 재시도 횟수 | 3 |

### Agent 설정

| 섹션 | 키 | 설명 | 기본값 |
|------|-----|------|--------|
| - | `supervisor_url` | Supervisor WebSocket URL | (필수) |
| - | `auth_token` | 인증용 공유 비밀 | (필수) |
| - | `opencode_port` | opencode serve 포트 | 4096 |
| `projects[]` | `name` | 프로젝트 이름 | (필수) |
| `projects[]` | `directory` | 프로젝트 디렉토리 경로 | (필수) |

---

## 문서

- **[DEPLOYMENT.md](docs/DEPLOYMENT.md)** — 프로덕션 모범 사례가 포함된 완전한 배포 가이드
- **[RUNBOOK.md](docs/RUNBOOK.md)** — 인시던트 대응 절차 및 문제 해결
- **[ROLLBACK.md](docs/ROLLBACK.md)** — 안전한 롤백 및 복구 절차
- **[DEVELOPMENT.md](docs/DEVELOPMENT.md)** — 기여자를 위한 개발 가이드
- **[제품 사양](Hal-o-swarm_Product_Spec_v1.1.md)** — 상세한 시스템 사양

---

## 성능

### 권장 하드웨어

| 컴포넌트 | CPU | 메모리 | 디스크 | 네트워크 |
|----------|-----|--------|--------|----------|
| Supervisor | 2+ 코어 | 2GB+ | 10GB+ SSD | 100Mbps+ |
| Agent | 2+ 코어 | 4GB+ | 20GB+ SSD | 100Mbps+ |

### 확장성

- **에이전트**: 50개 이상의 동시 에이전트로 테스트됨
- **세션**: 슈퍼바이저당 100개 이상의 동시 세션
- **이벤트**: 초당 10,000개 이상의 이벤트 처리량
- **데이터베이스**: SQLite가 수백만 개의 이벤트를 효율적으로 처리

### 최적화

```bash
# 오래된 이벤트 아카이브 (월별 실행)
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"

# 데이터베이스 정리 (아카이브 후 실행)
sqlite3 /var/lib/hal-o-swarm/supervisor.db "VACUUM;"

# 쿼리 최적화를 위한 분석
sqlite3 /var/lib/hal-o-swarm/supervisor.db "ANALYZE;"
```

---

## 문제 해결

### 일반적인 문제

#### Supervisor가 시작되지 않음

```bash
# 설정 확인
sudo journalctl -u hal-supervisor -n 50

# 포트 사용 가능 여부 확인
sudo lsof -i :8420

# 설정 테스트
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate
```

#### Agent가 연결할 수 없음

```bash
# Supervisor 실행 중인지 확인
sudo systemctl status hal-supervisor

# 인증 토큰 일치 확인
grep auth_token /etc/hal-o-swarm/supervisor.config.json
grep auth_token /etc/hal-o-swarm/agent.config.json

# 네트워크 연결 테스트
curl -v ws://슈퍼바이저-호스트:8420
```

#### 높은 메모리 사용량

```bash
# 현재 사용량 확인
ps aux | grep hal-supervisor

# systemd 메모리 제한 증가
sudo systemctl edit hal-supervisor
# 추가: MemoryMax=4G

# 오래된 데이터 아카이브
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-7 days');"
```

포괄적인 문제 해결 절차는 [RUNBOOK.md](docs/RUNBOOK.md)를 참조하세요.

---

## 기여

기여를 환영합니다! 다음 사항은 [DEVELOPMENT.md](docs/DEVELOPMENT.md)를 참조하세요:

- 개발 환경 설정
- 코드 스타일 가이드라인
- 테스트 요구사항
- Pull Request 프로세스

---

## 라이선스

MIT 라이선스 - 자세한 내용은 [LICENSE](LICENSE) 파일 참조

---

## 지원

- **문서**: [docs/](docs/) 디렉토리
- **이슈**: [GitHub Issues](https://github.com/bldg-7/hal-o-swarm/issues)
- **Discord**: #hal-o-swarm 채널
- **이메일**: support@example.com

---

## 변경 로그

### 버전 1.0.0 (2026년 2월)

**초기 릴리스**

- 세션 추적 및 이벤트 라우팅이 포함된 Supervisor
- WebSocket 재연결 및 이벤트 포워딩이 포함된 Agent
- Discord 슬래시 명령 통합 (9개 명령)
- Bearer 토큰 인증이 포함된 HTTP API
- Anthropic 및 OpenAI의 비용 집계
- 자동 개입 정책 엔진
- 원격 관리를 위한 CLI 도구 (halctl)
- TLS 지원 및 보안 강화
- Prometheus 메트릭 및 헬스 체크
- 포괄적인 배포 문서

---

**HAL-O-SWARM 팀이 ❤️로 만들었습니다**
