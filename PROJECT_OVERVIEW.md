# Project Overview

## System Overview

Multi-service platform that aggregates data from on-chain crypto exchanges (Hyperliquid, Lighter, Drift), stores it in a database, and provides a web dashboard for users to view their positions, trades, funding history, and analytics.

**Architecture:**
- Users connect exchange accounts via the dashboard
- Sync service periodically fetches data from exchanges
- Data is stored in PostgreSQL via Hasura GraphQL
- Dashboard displays data and analytics to users

---

## Components

### 1. `zif-lib` - Shared Go Library
**Purpose:** Exchange abstractions, database models, and shared utilities  
**Type:** Go module/package  
**Used by:** All backend services  
**Owner:** Dev1 + Dev2

### 2. `zif-sync-service` - Data Sync Service
**Purpose:** Periodically syncs exchange data → database  
**Type:** Go service (long-running)  
**Dependencies:** `zif-lib`, Hasura GraphQL  
**Owner:** Dev2

### 3. `zif-dashboard` - Web Application
**Purpose:** User-facing UI for account management and analytics  
**Type:** React + TypeScript (static app)  
**Dependencies:** Hasura GraphQL, Sync Service HTTP  
**Owner:** Dev3

### 4. `zif-infrastructure` - Infrastructure Config
**Purpose:** Database schema, Docker Compose, deployment configs  
**Type:** Config files, migrations  
**Owner:** Dev1

### 5. `zif-arbitrage-service` - Future Service
**Purpose:** Orderbook monitoring and arbitrage execution  
**Type:** Go service (future)  
**Dependencies:** `zif-lib`, Hasura GraphQL  
**Owner:** Dev1 (future)

---

## Data Flow

```
Users → Dashboard → Hasura GraphQL → PostgreSQL
                    ↑
Sync Service → Exchange APIs (via zif-lib) → Transform → Hasura GraphQL → PostgreSQL
```
