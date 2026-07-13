# ADR-003: Adopting Custom Metrics Collection and Request Counter Architecture

> Date: 2026-07-13  
> Status: Proposed

## Context

We need to implement a 'Request Counter' feature in the administrator back-office (`yoyaku_mate_admin`) of the queue management system (Rusui) to provide real-time request metrics (total requests over the last 24 hours, success rate, peak TPS) and a real-time API request log list.  
To implement this, we had to decide whether to introduce a commercial APM (Datadog, New Relic), directly build an open-source monitoring infrastructure like Prometheus+Grafana (self-hosting), build a custom monitoring engine from scratch, or build a custom lightweight monitoring module using the existing Go backend and MongoDB.

---

## Decision: Adopting Custom Collection using In-Memory Buffer (Batch Worker) and MongoDB TTL

To ensure cost efficiency for the early-stage service and to achieve dashboard integration within the single admin system, we will use a **custom metrics collection approach using Go in-memory buffering for async batch loading and MongoDB TTL (Time-To-Live) collections**, rather than integrating a separate external monitoring system.

---

## Comparison and Trade-offs by Architecture

### 1. Commercial APM Tools (Datadog, New Relic, etc.)
- **Pros**: Provides advanced visualization dashboards and real-time bottleneck monitoring simply by adding libraries, without separate development.
- **Cons**: High licensing and maintenance costs, which pose a significant financial burden for an early-stage startup. License costs scale proportionally with traffic volume, making costs uncontrollable.

### 2. Dedicated Open-Source Monitoring Solutions (Prometheus + Grafana) Self-Hosting
- **Pros**: Optimized for time-series data storage (TSDB), consuming the least server/DB resources when collecting large-scale traffic. Free software license.
- **Cons (Server Costs & Overhead)**:
  - **Increased Infrastructure Maintenance Costs**: We are currently using `fly.io`, and running this setup requires spinning up at least two additional container instances (one for Prometheus and one for Grafana), which immediately increases our hosting costs.
  - Requires separate web access and login credentials for the Grafana dashboard, reducing admin integration.
  - Only aggregates numerical metrics, so it cannot retrieve raw log details for specific requests (e.g., failed log details from a specific IP).

### 3. Developing a Professional Monitoring Engine from Scratch (Go + Custom TSDB)
- **Pros**: Allows building a fully customized independent collection system tailored 100% to our service.
- **Cons (Infrastructure & Opportunity Costs)**:
  - A clear case of **over-engineering** that deviates significantly from the core business domain (queue and reservation management).
  - Optimization and maintenance of the custom monitoring engine demand extra CPU/memory resources and separate monitoring daemons, leading to wasted server resources and **increased indirect server costs**.

### 4. Custom Lightweight Monitoring (In-Memory Buffer + MongoDB) - **[CHOSEN]**
- **Pros**:
  - **Zero Server Costs**: No need to spin up additional servers or purchase commercial library licenses. It shares the existing infrastructure (Go API server + MongoDB Atlas free/basic tier), resulting in zero additional cost.
  - **Admin Integration**: Can be seamlessly rendered inside the existing React admin web dashboard.
  - **Raw Logs Provided**: Enables fast debugging by allowing administrators to directly query raw log details such as API request duration and Client IP.
  - **Minimal Application Impact**: Isolates the overhead on the API request processing flow by performing bulk insertions via a 5-second background Goroutine batch worker.
- **Cons (Potential Risk)**:
  - **Increased Storage Server Costs Under Heavy Traffic**: Since all API request logs are written to MongoDB, disk space usage may accumulate under heavy traffic, potentially raising MongoDB Atlas storage costs.
  - **Mitigation**: Configured a **3-day (259,200 seconds) TTL index** on the `request_logs` collection so that old data is automatically deleted, capping disk space usage and preventing storage cost increases.

---

## Consequences

- **Short-Term Consequences**: We can quickly implement straightforward API monitoring and debugging without additional infrastructure costs.
- **Long-Term Consequences**: In the future, if the number of registered stores and user traffic surges (exceeding thousands of requests per second), we may need to migrate metric data to a time-series DB (like Prometheus) or a separate APM tool to alleviate MongoDB write load. The current structure serves as a practical, transitionary architecture for this stage.

---

## Related Documents

- [Request Dashboard UI Specification (Admin UI)](../features/request-counter.ko.md)
