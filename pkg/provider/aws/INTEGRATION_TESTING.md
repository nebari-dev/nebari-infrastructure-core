# Integration Testing for AWS Provider

This directory contains integration tests for the AWS provider using LocalStack for mocking AWS services.

## Prerequisites

1. **Docker**: Integration tests require Docker to run LocalStack containers
   ```bash
   docker --version
   ```

2. **Go 1.22+**: Required for running tests with build tags

## Running Integration Tests

### Run All Integration Tests

```bash
# Using make (recommended)
make test-integration

# Using go test directly
go test -v -tags=integration ./pkg/provider/aws -timeout 30m
```

### Run Specific Integration Tests

```bash
# Run only VPC tests
go test -v -tags=integration ./pkg/provider/aws -run TestIntegration_VPC

# Run only IAM tests
go test -v -tags=integration ./pkg/provider/aws -run TestIntegration_IAM

# Run discovery tests
go test -v -tags=integration ./pkg/provider/aws -run TestIntegration.*Discovery
```

### Skip Integration Tests

Integration tests are automatically skipped in short mode:

```bash
# This will skip integration tests
go test -short ./pkg/provider/aws
```

## Test Structure

### Integration Test Files

Integration tests use the `//go:build integration` build tag and are in files ending with `_integration_test.go` or marked with the build tag.

**File:** `integration_test.go`
- Uses LocalStack via testcontainers-go
- Mocks AWS EC2, IAM, and STS services
- Tests actual AWS SDK interactions

### Test Categories

1. **VPC Tests** (`TestIntegration_VPC*`)
   - VPC creation
   - VPC discovery
   - VPC reconciliation
   - Subnet creation
   - Internet gateway setup
   - NAT gateway configuration

2. **IAM Tests** (`TestIntegration_IAM*`)
   - IAM role creation (cluster role, node role)
   - IAM policy attachment
   - IAM role discovery
   - IAM role deletion

3. **Tag Tests** (`TestIntegration_Tag*`)
   - Tag generation
   - Tag application
   - Tag-based resource discovery

4. **Reconciliation Tests** (`TestIntegration_Reconcile*`)
   - No existing resources scenario
   - Existing resources with matching config
   - CIDR mismatch detection

## LocalStack Configuration

Tests use LocalStack 4.0 with the following services enabled:
- EC2 (VPC, subnets, security groups, etc.)
- IAM (roles, policies)
- STS (for role assumption)

**Note:** EKS is not fully supported in LocalStack Community Edition, so EKS-specific integration tests require real AWS or LocalStack Pro.

## Cleanup

Integration tests automatically clean up resources using deferred cleanup functions:

```go
testCtx.AddCleanup(func() {
    // Cleanup logic
})
defer testCtx.Cleanup()
```

If a test fails, cleanup still runs to avoid leaving resources in LocalStack.

## Debugging Integration Tests

### View LocalStack Logs

```bash
# If a test is running, you can view LocalStack container logs
docker logs $(docker ps -q --filter ancestor=localstack/localstack:4.0)
```

### Enable Verbose Output

```bash
# Run tests with verbose output
go test -v -tags=integration ./pkg/provider/aws -run TestIntegration_VPC
```

### Keep Container Running for Debugging

Modify the test to comment out `defer testCtx.Cleanup()` temporarily:

```go
testCtx := SetupLocalStack(t)
// defer testCtx.Cleanup()  // Comment out to keep container running
```

Then inspect the container:

```bash
docker ps
docker exec -it <container-id> bash
```

## Timeout Configuration

Integration tests have a default timeout of 30 minutes. You can adjust this:

```bash
go test -v -tags=integration ./pkg/provider/aws -timeout 45m
```

## CI/CD Integration

### GitHub Actions Example

```yaml
integration-tests:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: "1.22"
    - name: Run integration tests
      run: make test-integration
```

## Limitations

1. **LocalStack Community Edition** has limited EKS support
2. **Performance**: Tests are slower than unit tests (30s-2min per test)
3. **Docker required**: CI environments must have Docker available
4. **Resource limits**: LocalStack may not perfectly emulate all AWS behaviors

## Best Practices

1. **Always use cleanup functions** to avoid resource leaks
2. **Test one thing at a time** - keep tests focused
3. **Use descriptive test names** following `TestIntegration_<Component>_<Scenario>` pattern
4. **Add context** with `t.Logf()` for debugging
5. **Handle errors explicitly** - don't ignore cleanup errors

## Troubleshooting

### "Cannot connect to Docker daemon"

```bash
# Ensure Docker is running
sudo systemctl start docker

# Or use Docker Desktop
```

### "Container failed to start"

Check if port 4566 is already in use:

```bash
lsof -i :4566
```

### "Timeout waiting for resource"

Increase the timeout or check LocalStack logs for errors.

## Further Reading

- [Testcontainers Go Documentation](https://golang.testcontainers.org/)
- [LocalStack Documentation](https://docs.localstack.cloud/)
- [AWS SDK for Go v2 Testing Guide](https://aws.github.io/aws-sdk-go-v2/docs/unit-testing/)
