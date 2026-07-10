# 개발 과정에서 배운 것들 (Lessons Learned)

> 작성일: 2026-07-10

---

## Goroutine 리크 방지

SSE 스트리밍 연결 시, 클라이언트의 예상치 못한 연결 종료(`Context Done`)를 즉시 감지하지 못하면 Broker 내부의 Goroutine이 소멸되지 않고 누적되는 문제가 발생합니다.

`Context`와 `Select` 채널을 활용한 라이프사이클 관리로 이를 방지합니다.

```go
for {
    select {
    case <-r.Context().Done():
        return  // 클라이언트 연결 종료 감지 → Goroutine 종료
    case msg := <-clientChan:
        fmt.Fprintf(w, "data: %s\n\n", msg)
        w.(http.Flusher).Flush()
    }
}
```

→ [SSE 구현 상세](../implementation/sse.ko.md)

---

## Rate Limiter 임계값 조정

모바일/웹 클라이언트는 여러 에셋과 API를 동시에 호출하는 특성이 있어, 단순히 낮은 임계값을 설정하면 정상 사용자도 차단됩니다.

- **현재 설정**: 5 req/s per IP, Burst 10
- Burst 값을 통해 순간적인 병렬 요청(이미지 로딩 등)은 허용하면서 지속적인 과다 요청은 차단

---

## NoSQL 정합성과 안전한 예외 처리

MongoDB 드라이버의 쿼리 작성 시 예외 처리를 빠짐없이 구현하고, 에러 변수를 명확하게 캐치하여 반환합니다. 트랜잭션 없는 단일 문서 쓰기 작업의 무결성은 유효성 검증(Validation)으로 보완합니다.

---

## 클라이언트 등록 시각 보호

서버 도달 시각이 아닌, **클라이언트가 실제로 "등록" 버튼을 누른 시각**을 `registration_time`으로 저장합니다. 서버에서 이를 덮어쓰지 않아 네트워크 지연에 관계없이 대기 순서의 시간적 정합성을 보장합니다.
