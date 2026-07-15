# 002: 실시간 접속자 중복 카운트 방지 (IP 포트 및 IPv6 단일화)

> 작성일: 2026-07-14  
> 상태: 해결됨 (Resolved)  
> 관련 파일: [`metrics/middleware.go`](../../metrics/middleware.go)

---

## 현상 (Symptom)

어드민 백오피스(`yoyaku_mate_admin`)의 활성 사용자 대시보드에서 다음과 같은 비정상 카운트 증상이 관찰되었습니다.
1. **지속 증가 현상**: 동일 기기/브라우저에서 대시보드를 켜두었음에도 프론트엔드가 5초마다 API 폴링 요청을 보낼 때마다 `CURRENT ACTIVE USERS` 수치가 1씩 계속 올라갔습니다.
2. **다중 감지 현상**: 로컬 환경에서 분명 1인만 개발 접속 중임에도 불구하고 접속자 수가 `2`로 표시되었습니다.

---

## 원인 분석 (Root Cause)

### 1. 임시 포트(Port) 번호가 포함된 식별자 문제
Go 백엔드의 `r.RemoteAddr`은 단순 IP가 아닌 `IP:Port` 규격의 주소 문자열(예: `127.0.0.1:54321`)을 리턴합니다. 
브라우저가 HTTP 폴링을 시도할 때마다 클라이언트 포트는 **임시 포트(Ephemeral Port)** 규칙에 의해 임의의 번호로 매번 바뀝니다. 
포트 번호를 떼어내지 않고 그대로 유저 식별자 키로 맵에 등록했기 때문에, 동일한 기기에서 오는 요청임에도 서버는 매번 다른 신규 유저가 인입된 것으로 판단하여 카운트가 계속 늘어났습니다.

### 2. IPv4와 IPv6 루프백 주소의 병렬 인식 문제
로컬 가동(localhost) 환경에서 브라우저가 도메인을 해석하며 IPv4 루프백 주소인 `127.0.0.1`과 IPv6 루프백 주소인 `::1`을 번갈아 가며 요청을 전송하였습니다. 
서버 측 맵에는 두 주소가 서로 다른 문자열이므로 별개의 고유 접속자로 집계되어 최종 결과가 `2`로 표출되었습니다.

---

## 해결 방안 (Solution)

### 1. `net.SplitHostPort`를 사용한 포트 격리
미들웨어에서 IP 주소를 획득할 때 Go 표준 라이브러리인 `net.SplitHostPort`를 이용하여 포트부를 제외하고 **순수 IP 부문만 동적으로 슬라이싱**하도록 변경했습니다.

### 2. IPv6 루프백 주소의 단일화 (Normalization)
로컬 개발 환경 및 듀얼 스택 네트워크 환경에서의 접속자 카운트 무결성을 위해, 수집된 IP가 `::1`인 경우 이를 `127.0.0.1`로 맵핑(통일)해주는 정제 필터를 추가했습니다.

```go
// MetricsMiddleware 내 IP 정제 구현부
clientIP := r.Header.Get("X-Forwarded-For")
if clientIP == "" {
    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    if err == nil {
        clientIP = ip
    } else {
        clientIP = r.RemoteAddr
    }
} else {
    ips := strings.Split(clientIP, ",")
    clientIP = strings.TrimSpace(ips[0])
}

// IPv6 루프백을 IPv4 루프백으로 통일하여 중복 방지
if clientIP == "::1" {
    clientIP = "127.0.0.1"
}
```

---

## 결과 및 정리 (Consequences)
- **정밀한 식별**: 브라우저의 지속적인 폴링 요청에도 동일 IP로 식별되어 중복 카운트가 완벽히 해결되었습니다.
- **다중 도메인 대응**: 로컬 접속 경로(IPv4/IPv6)에 관계없이 루프백 주소가 통합되어 정확히 `1`명으로 정상 트래킹됩니다.
- **교훈**: 소켓이나 네트워크 패킷 기반의 접속자 트래킹 시, 포트 번호 분리와 루프백(개발 환경) 정규화 처리는 기본 예외처리 항목으로 다루어져야 합니다.

---

## 관련 문서
- [기능 사양서: 활성 사용자 대시보드](../features/active-user-dashboard.ko.md)
- [구현 상세서: 활성 사용자 트래킹](../implementation/active-user-tracking.ko.md)
- [ADR-004: 인메모리 슬라이딩 윈도우 및 일별 활성 사용자 컬렉션을 활용한 접속자 트래킹](../decisions/ADR-004-active-user-tracking.ko.md)
