# 100M Row PostgreSQL Benchmark

High-performance streaming ingestion, concurrent B-Tree indexing, and query profiling across **100M rows** in PostgreSQL 18 on a single consumer laptop.

---

## Hardware Environment

* **CPU:** 12th Gen Intel Core i5-1240P (12 cores, 16 threads, up to 4.4 GHz)
* **RAM:** 16 GB DDR4/DDR5
* **Disk:** Consumer NVMe SSD
* **Database Engine:** PostgreSQL 18.1 (Native Windows Service)
* **Backend Runtime:** Go 1.26 with `pgx/v5` connection pool

---

## Benchmark Results (Grafana k6)

Tested under concurrent load using Grafana k6 (`loadtest.js`): **100 concurrent Virtual Users (VUs)** looping across random composite index queries, deep keyset seeks across row #95M+, user activity lookups, and comment thread fetches.

| Metric | Result | Target | Status |
| :--- | :--- | :--- | :--- |
| **Throughput** | **4,282.7 req/s** | &gt; 500 req/s | **PASSED** |
| **Median Latency** | **1.97 ms** | &lt; 10 ms | **PASSED** |
| **p90 Latency** | **4.47 ms** | &lt; 50 ms | **PASSED** |
| **p95 Latency** | **5.99 ms** | &lt; 100 ms | **PASSED** |
| **Max Spike** | **89.8 ms** | &lt; 500 ms | **PASSED** |
| **Error Rate** | **0.00%** (0 / 128,579) | 0.00% | **PASSED** |

### k6 Terminal Output

```text
  █ THRESHOLDS 

    http_req_duration
    ✓ 'p(95)<100' p(95)=5.99ms

    http_req_failed
    ✓ 'rate<0.01' rate=0.00%

  █ TOTAL RESULTS 

    checks_total.......: 128579  4282.667713/s
    checks_succeeded...: 100.00% 128579 out of 128579
    checks_failed......: 0.00%   0 out of 128579

    ✓ user lookup status is 200
    ✓ comments status is 200
    ✓ stories status is 200
    ✓ raw cursor status is 200

    HTTP
    http_req_duration..: avg=2.53ms min=0s med=1.97ms max=89.8ms p(90)=4.47ms p(95)=5.99ms
    http_req_failed....: 0.00%  0 out of 128579
    http_reqs..........: 128579 4282.667713/s

    NETWORK
    data_received......: 317 MB 11 MB/s
    data_sent..........: 13 MB  425 kB/s
```

---

## How to Run & Reproduce

### 1. Prerequisites
* Go 1.24+ or Go 1.26
* PostgreSQL 16, 17, or 18
* [Grafana k6](https://k6.io/) (for load testing)

### 2. Configure Environment
Copy `.env.example` to `.env` in the project root:
```bash
cp .env.example .env
```

Configure your credentials:
```env
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=rowageddon
POSTGRES_PASSWORD=your_password_here
```

### 3. Initialize Schema
Run the base table migration (table only, indexes omitted during raw ingestion):
```bash
psql -U postgres -d rowageddon -f migrations/0001_create_hn_items.sql
```

### 4. Stream Ingestion (100 Million Rows)
Run the binary streaming loader using PostgreSQL's native `COPY` protocol via `pgx.CopyFrom`:
```bash
go run ./cmd/load -count=100000000 -step=2000000
```
*(The `-step=2000000` flag is just for visibility, loading 100M rows takes several minutes, so it prints live throughput and progress every 2M records instead of leaving you staring at a silent terminal wondering if it froze.)*

### 5. Build Indexes Concurrently
Once the table heap is fully loaded, build secondary indexes concurrently:
```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_author ON hn_items (author, created_at DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_comments_parent ON hn_items (parent_id, created_at) WHERE parent_id IS NOT NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_stories_top_scoring ON hn_items (score DESC, created_at DESC) WHERE item_type = 'story';
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_active_stories ON hn_items (score DESC, created_at DESC) WHERE item_type = 'story' AND deleted_at IS NULL;
```

### 6. Start the Server & Workbench
```bash
go run ./cmd/server
```
* **Workbench:** Open [http://localhost:8080/](http://localhost:8080/) for the interactive heap explorer, query profiler, and live benchmark graphs.
* **And if you are intersted i have my notes in there :):**  [http://localhost:8080/blog.html](http://localhost:8080/blog.html).


### 7. Run the k6 Load Test
```bash
k6 run --vus 100 --duration 30s loadtest.js
```
