package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

// mockAPIError implements smithy.APIError for testing.
type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) Error() string                 { return fmt.Sprintf("api error %s: %s", e.code, e.message) }
func (e *mockAPIError) ErrorCode() string             { return e.code }
func (e *mockAPIError) ErrorMessage() string          { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

// mockEC2Client implements EC2Client for testing.
type mockEC2Client struct {
	DescribeSecurityGroupsFunc     func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DeleteSecurityGroupFunc        func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	RevokeSecurityGroupIngressFunc func(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
}

func (m *mockEC2Client) DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if m.DescribeSecurityGroupsFunc != nil {
		return m.DescribeSecurityGroupsFunc(ctx, params, optFns...)
	}
	return &ec2.DescribeSecurityGroupsOutput{}, nil
}

func (m *mockEC2Client) DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	if m.DeleteSecurityGroupFunc != nil {
		return m.DeleteSecurityGroupFunc(ctx, params, optFns...)
	}
	return &ec2.DeleteSecurityGroupOutput{}, nil
}

func (m *mockEC2Client) RevokeSecurityGroupIngress(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error) {
	if m.RevokeSecurityGroupIngressFunc != nil {
		return m.RevokeSecurityGroupIngressFunc(ctx, params, optFns...)
	}
	return &ec2.RevokeSecurityGroupIngressOutput{}, nil
}

func TestCleanupK8sELBSecurityGroups(t *testing.T) {
	tagKey := "kubernetes.io/cluster/my-cluster"

	t.Run("deletes matching security groups", func(t *testing.T) {
		var deletedIDs []string
		mock := &mockEC2Client{
			DescribeSecurityGroupsFunc: func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
				// Check which filter is being used
				for _, f := range params.Filters {
					if *f.Name == "group-name" {
						return &ec2.DescribeSecurityGroupsOutput{
							SecurityGroups: []ec2types.SecurityGroup{
								{
									GroupId:   aws.String("sg-111"),
									GroupName: aws.String("k8s-elb-abc123"),
									Tags: []ec2types.Tag{
										{Key: aws.String(tagKey), Value: aws.String("owned")},
									},
								},
							},
						}, nil
					}
					// revokeReferencingRules call — no referencing SGs
					if *f.Name == "ip-permission.group-id" {
						return &ec2.DescribeSecurityGroupsOutput{}, nil
					}
				}
				return &ec2.DescribeSecurityGroupsOutput{}, nil
			},
			DeleteSecurityGroupFunc: func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				deletedIDs = append(deletedIDs, *params.GroupId)
				return &ec2.DeleteSecurityGroupOutput{}, nil
			},
		}

		deleted, err := cleanupK8sELBSecurityGroups(context.Background(), mock, tagKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted != 1 {
			t.Errorf("expected 1 deleted, got %d", deleted)
		}
		if len(deletedIDs) != 1 || deletedIDs[0] != "sg-111" {
			t.Errorf("expected sg-111 to be deleted, got %v", deletedIDs)
		}
	})

	t.Run("skips security groups without cluster tag", func(t *testing.T) {
		mock := &mockEC2Client{
			DescribeSecurityGroupsFunc: func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
				return &ec2.DescribeSecurityGroupsOutput{
					SecurityGroups: []ec2types.SecurityGroup{
						{
							GroupId:   aws.String("sg-222"),
							GroupName: aws.String("k8s-elb-def456"),
							Tags: []ec2types.Tag{
								{Key: aws.String("kubernetes.io/cluster/other-cluster"), Value: aws.String("owned")},
							},
						},
					},
				}, nil
			},
			DeleteSecurityGroupFunc: func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				t.Fatal("DeleteSecurityGroup should not be called")
				return nil, nil
			},
		}

		deleted, err := cleanupK8sELBSecurityGroups(context.Background(), mock, tagKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deleted, got %d", deleted)
		}
	})

	t.Run("no security groups found", func(t *testing.T) {
		mock := &mockEC2Client{}

		deleted, err := cleanupK8sELBSecurityGroups(context.Background(), mock, tagKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deleted, got %d", deleted)
		}
	})
}

func TestRevokeReferencingRules(t *testing.T) {
	t.Run("revokes rules in referencing security groups", func(t *testing.T) {
		var revokedCalls []string
		mock := &mockEC2Client{
			DescribeSecurityGroupsFunc: func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
				return &ec2.DescribeSecurityGroupsOutput{
					SecurityGroups: []ec2types.SecurityGroup{
						{
							GroupId: aws.String("sg-node"),
							IpPermissions: []ec2types.IpPermission{
								{
									IpProtocol: aws.String("tcp"),
									FromPort:   aws.Int32(80),
									ToPort:     aws.Int32(80),
									UserIdGroupPairs: []ec2types.UserIdGroupPair{
										{GroupId: aws.String("sg-elb")},
									},
								},
							},
						},
					},
				}, nil
			},
			RevokeSecurityGroupIngressFunc: func(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error) {
				revokedCalls = append(revokedCalls, *params.GroupId)
				if len(params.IpPermissions) != 1 {
					t.Errorf("expected 1 permission to revoke, got %d", len(params.IpPermissions))
				}
				if *params.IpPermissions[0].UserIdGroupPairs[0].GroupId != "sg-elb" {
					t.Errorf("expected revoke of sg-elb reference, got %s", *params.IpPermissions[0].UserIdGroupPairs[0].GroupId)
				}
				return &ec2.RevokeSecurityGroupIngressOutput{}, nil
			},
		}

		err := revokeReferencingRules(context.Background(), mock, "sg-elb")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(revokedCalls) != 1 || revokedCalls[0] != "sg-node" {
			t.Errorf("expected revoke on sg-node, got %v", revokedCalls)
		}
	})

	t.Run("no referencing security groups", func(t *testing.T) {
		mock := &mockEC2Client{}

		err := revokeReferencingRules(context.Background(), mock, "sg-elb")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("skips permissions without matching group ID", func(t *testing.T) {
		mock := &mockEC2Client{
			DescribeSecurityGroupsFunc: func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
				return &ec2.DescribeSecurityGroupsOutput{
					SecurityGroups: []ec2types.SecurityGroup{
						{
							GroupId: aws.String("sg-node"),
							IpPermissions: []ec2types.IpPermission{
								{
									IpProtocol: aws.String("tcp"),
									FromPort:   aws.Int32(443),
									ToPort:     aws.Int32(443),
									UserIdGroupPairs: []ec2types.UserIdGroupPair{
										{GroupId: aws.String("sg-other")},
									},
								},
							},
						},
					},
				}, nil
			},
			RevokeSecurityGroupIngressFunc: func(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error) {
				t.Fatal("RevokeSecurityGroupIngress should not be called")
				return nil, nil
			},
		}

		err := revokeReferencingRules(context.Background(), mock, "sg-elb")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestDeleteSecurityGroupWithRetry(t *testing.T) {
	tests := []struct {
		name         string
		deleteFunc   func(attempts *int) func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
		cancelAfter  int // Cancel context after this many attempts (0 = no cancel)
		wantErr      bool
		wantAttempts int
	}{
		{
			name: "succeeds on first attempt",
			deleteFunc: func(attempts *int) func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				return func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					*attempts++
					return &ec2.DeleteSecurityGroupOutput{}, nil
				}
			},
			wantErr:      false,
			wantAttempts: 1,
		},
		{
			name: "retries on DependencyViolation then succeeds",
			deleteFunc: func(attempts *int) func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				return func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					*attempts++
					if *attempts <= 2 {
						return nil, &mockAPIError{code: "DependencyViolation", message: "resource has a dependent object"}
					}
					return &ec2.DeleteSecurityGroupOutput{}, nil
				}
			},
			wantErr:      false,
			wantAttempts: 3,
		},
		{
			name: "fails immediately on non-DependencyViolation error",
			deleteFunc: func(attempts *int) func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				return func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					*attempts++
					return nil, &mockAPIError{code: "InvalidGroup.NotFound", message: "sg not found"}
				}
			},
			wantErr:      true,
			wantAttempts: 1,
		},
		{
			name: "fails immediately on non-smithy error",
			deleteFunc: func(attempts *int) func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				return func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					*attempts++
					return nil, fmt.Errorf("network error")
				}
			},
			wantErr:      true,
			wantAttempts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			mock := &mockEC2Client{
				DeleteSecurityGroupFunc: tt.deleteFunc(&attempts),
			}

			err := deleteSecurityGroupWithRetry(context.Background(), mock, "sg-111")
			if (err != nil) != tt.wantErr {
				t.Errorf("deleteSecurityGroupWithRetry() error = %v, wantErr %v", err, tt.wantErr)
			}
			if attempts != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", attempts, tt.wantAttempts)
			}
		})
	}

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0
		mock := &mockEC2Client{
			DeleteSecurityGroupFunc: func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				attempts++
				cancel() // Cancel after first attempt
				return nil, &mockAPIError{code: "DependencyViolation", message: "has a dependent object"}
			},
		}

		err := deleteSecurityGroupWithRetry(ctx, mock, "sg-111")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt before cancellation, got %d", attempts)
		}
	})
}
