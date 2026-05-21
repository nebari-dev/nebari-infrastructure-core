# Azure Provider Integration Testing

Integration tests for the Azure provider run against a real Azure subscription.
There is no LocalStack-equivalent emulator for AKS/ARM.

## Prerequisites

- `AZURE_SUBSCRIPTION_ID` set to a sub where you can create AKS clusters.
- One of:
  - `az login` completed in the shell
  - Service principal env vars: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`

## Running

```bash
go test -v -tags=integration -timeout 60m ./pkg/provider/azure/...
```

Tests gated behind the `integration` build tag are not run by default `make test`.

## Cost

Each test cycle provisions a minimal AKS cluster (Standard_B2s, single-node
system pool, single user pool) and tears it down. Expect a few cents to a
couple of dollars per run depending on region and how long it stays up.
