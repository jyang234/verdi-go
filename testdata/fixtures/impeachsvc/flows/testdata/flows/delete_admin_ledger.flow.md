sequenceDiagram
    participant Client as Client
    participant impeachsvc as impeachsvc
    participant postgres as postgres
    Client->>impeachsvc: HTTP DELETE /admin/ledger
    impeachsvc->>postgres: DB postgres DELETE ledger
