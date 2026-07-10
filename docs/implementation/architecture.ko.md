# 서버 아키텍처 개요

> 최종 수정: 2026-07-10

## 기술 스택

| 항목 | 기술 |
|------|------|
| 언어 | Go |
| HTTP 라우터 | `gorilla/mux` |
| DB | MongoDB Atlas |
| 인증 | Firebase Auth |
| 파일 스토리지 | Cloudflare R2 (MinIO 호환 클라이언트) |
| 배포 | fly.io |
| Rate Limiting | `tollbooth` (5 req/s per IP, burst 10) |
| CORS | `rs/cors` |

---

## 디렉토리 구조

```
yoyaku_mate_server/
│
├── main.go              # 진입점: DB 초기화, 미들웨어, 서버 시작
│
├── handlers/            # HTTP 핸들러 (요청 파싱 / 응답 반환)
│   ├── router.go        # 라우터 등록
│   ├── waiting_list_handler.go
│   ├── sign_up_handler.go
│   ├── statistics_handler.go
│   └── ...
│
├── data/                # 데이터 접근 계층 (MongoDB 쿼리)
│   ├── waiting_list.go
│   ├── counters.go      # Atomic Counter
│   ├── store_info.go
│   └── ...
│
├── models/              # Go 구조체 (DB 스키마 / JSON 직렬화)
│   ├── waiting_list_model.go
│   └── ...
│
├── events/              # SSE Broker (in-memory pub/sub)
│   ├── broker.go        # Store 단위 브로드캐스트
│   └── waiting_user_broker.go  # 개별 손님 단위
│
├── auth/                # 인증 관련 로직
│   ├── firebase_auth.go # Firebase ID Token 검증
│   └── login_token.go   # 중복 로그인 방지 토큰 검증
│
├── config/              # 환경 변수 로딩
├── db/                  # MongoDB 연결 관리
54: └── utils/               # 공통 유틸리티 (JSON 응답, HMAC 토큰 등)
```

---

## 레이어 구조

```
Request
    │
    ▼
[handlers]      HTTP 파싱, 인증 체크, 비즈니스 룰 검증
    │
    ▼
[data]          MongoDB 쿼리 (try-catch 패턴)
    │
    ▼
[models]        Go 구조체 ↔ BSON/JSON 직렬화
    │
    ▼
MongoDB Atlas
```

---

## 인증 흐름

```
Request
    │
    ├── Authorization 헤더 없음 → 공개 엔드포인트 (손님 QR 등록 등)
    │
    └── Authorization: Bearer <id_token>
            │
            ▼
        auth.VerifyIDToken()  →  Firebase UID 추출
            │
            ▼
        data.GetUserByFirebaseUID()  →  내부 User 객체
            │
            ▼
        auth.VerifyLoginToken()  →  X-Login-Token 헤더 검증
            │                        (중복 로그인 방지)
            ▼
        data.CheckUserStorePermission()  →  점포 권한 확인
```

---

## 주요 API 그룹

| 경로 | 설명 |
|------|------|
| `/api/waiting-list` | 대기열 CRUD + SSE 스트림 |
| `/api/menu-list`, `/api/provider_menu` | 메뉴 관리 |
| `/api/provider_user`, `/api/provider_store` | 유저/점포 정보 |
| `/api/auth/signup`, `/api/stores/add` | 회원가입 / 점포 등록 |
| `/api/admin/*` | 관리자 전용 (점포 승인 등) |
| `/api/stores/{storeId}/staff` | 스태프 관리 |
| `/api/statistics` | 대기 통계 |
| `/api/public/ai-chat` | AI 채팅 (공개) |

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [SSE 구현](./sse.ko.md)
- [멱등성 구현](./idempotency.ko.md)
- [Atomic Counter](./atomic-counter.ko.md)
