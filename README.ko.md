# ⚙️ Yoyaku Mate - 통합 API 백엔드 서버 (Go Backend)

> **Yoyaku Mate** 백엔드 서버는 실시간 대기열 서비스의 전체 비즈니스 로직을 처리하는 **Go 기반 RESTful API 서버**입니다. MongoDB Atlas 데이터베이스 모델 설계, Cloudflare R2 파일 업로드, Firebase Admin SDK 연동을 통한 사용자 인증 처리 및 실시간 동기화를 지원합니다.

---

## 🛠 Tech Stack (기술 스택)

- **Language:** Go (Golang) 1.23
- **Router:** `gorilla/mux` (HTTP 멀티플렉서 라우팅)
- **Database:** MongoDB Atlas (NoSQL 데이터 저장소)
- **Object Storage:** Cloudflare R2 (S3 호환 고성능 파일/에셋 저장소)
- **External Integration:**
  - Firebase Admin SDK (사용자 인증 토큰 검증)
- **Security & Middleware:**
  - `rs/cors` (CORS 정책 관리)
  - `didip/tollbooth` (Rate Limiting 기반 API 과도 요청 제한)
- **Deployment:** Fly.io (Docker 컨테이너 기반 글로벌 가상 서버 호스팅)

---

## ✨ Key Features (핵심 기능)

- **대기열 실시간 관리 API:** 매장 대기 신청, 순서 변경, 대기 상태 변경 로직 처리 및 데이터 트랜잭션 보장.
- **Firebase JWT 인증 연동:** 클라이언트(앱/웹)에서 전달한 Firebase 토큰 유효성을 백엔드 단에서 검증하여 안전한 데이터 읽기/쓰기를 보장합니다.
- **Cloudflare R2 연동:** 정적 파일 업로드 시 S3 API 호환 라이브러리를 통해 파일 저장을 효율적으로 대행합니다.
- **안전한 API 구조:** 속도 제한(Rate Limit) 미들웨어를 도입하여 비정상적인 디도스(DDoS) 형태의 과도한 API 공격을 제한합니다.

---

## 📂 Project Structure (폴더 구조)

```bash
├── auth/           # Firebase Admin SDK 연동 인증 및 토큰 발급/검증 로직
├── config/         # JSON 파일 및 환경 변수를 통한 설정 관리 로직 (config.go)
├── data/           # 정적 번역 템플릿 및 데이터 리소스
├── db/             # MongoDB Atlas 연결 수립 및 드라이버 설정 (mongo.go)
├── handlers/       # 라우터 엔드포인트별 비즈니스 핸들러 함수
├── models/         # MongoDB 스키마에 매핑되는 Go 구조체 정의
├── utils/          # HMAC 토큰 발급, 로거, 날짜 가이드 유틸리티 함수
├── Dockerfile      # Fly.io 배포를 위한 도커 멀티스테이지 빌드 환경 구성
├── fly.toml        # Fly.io 애플리케이션 가상 서버 설정
├── main.go         # 서버 실행 진입점 및 미들웨어/라우팅 등록
└── go.mod          # 의존성 모듈 의존 파일
```

---

## 🚀 Getting Started (시작 가이드)

### 1. 환경 변수 설정
로컬 및 배포 환경 실행을 위해 아래 환경 변수 설정이 필요합니다.

- `PORT`: 서버 포트 (기본값: `8080`)
- `MONGODB_URI`: MongoDB Atlas 접속 경로 주소
- `MONGODB_DATABASE`: 데이터베이스 이름
- `HMAC_SECRET`: 토큰 발급에 사용할 고유 보안 암호 키
- `R2_ACCOUNT_ID`: Cloudflare R2 계정 ID
- `R2_ACCESS_KEY` & `R2_SECRET_KEY`: R2 스토리지 접속 키
- `R2_ASSETS_BUCKET_NAME` & `R2_BIZ_BUCKET_NAME`: 업로드 전용 R2 버킷 명칭

### 2. 로컬 실행
개발용 설정 파일인 `config/development.json`과 `config/serviceAccountKey.json`이 로컬 환경의 `config/` 디렉토리에 존재해야 합니다.

```bash
# 의존성 모듈 설치
go mod download

# 서버 구동
go run main.go
```
서버가 실행되면 `http://localhost:8080` 포트로 API 서비스가 동작합니다.

---

## 🐋 Deploy (배포)

본 백엔드 프로젝트는 **Fly.io**에 Docker 기반 멀티스테이지 빌드 방식으로 배포됩니다.
GitHub Actions 가입 시 `fly-deploy.yml` 워크플로우를 통하여 `main` 브랜치 푸시 시 자동 배포가 유발됩니다.

```bash
# 로컬에서 Fly.io CLI 배포 실행 시
flyctl deploy
```
