# `zif-lib` - Detailed Documentation

## Purpose

Shared Go module providing:
1. Exchange abstractions (interfaces and implementations for Hyperliquid, Lighter, Drift)
2. Database models (Go structs matching the database schema)
3. Common utilities (rate limiting, retry logic, error handling)

**This is a pure library** - no services, no main functions, no business logic. It's imported by other services.
