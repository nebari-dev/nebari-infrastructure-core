package aws

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
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

// mockELBClient implements ELBClient for testing.
type mockELBClient struct {
	DescribeLoadBalancersFunc func(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	DescribeTagsFunc          func(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error)
	DeleteLoadBalancerFunc    func(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error)
}

func (m *mockELBClient) DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	if m.DescribeLoadBalancersFunc != nil {
		return m.DescribeLoadBalancersFunc(ctx, params, optFns...)
	}
	return &elb.DescribeLoadBalancersOutput{}, nil
}

func (m *mockELBClient) DescribeTags(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error) {
	if m.DescribeTagsFunc != nil {
		return m.DescribeTagsFunc(ctx, params, optFns...)
	}
	return &elb.DescribeTagsOutput{}, nil
}

func (m *mockELBClient) DeleteLoadBalancer(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error) {
	if m.DeleteLoadBalancerFunc != nil {
		return m.DeleteLoadBalancerFunc(ctx, params, optFns...)
	}
	return &elb.DeleteLoadBalancerOutput{}, nil
}

// mockELBv2Client implements ELBv2Client for testing.
type mockELBv2Client struct {
	DescribeLoadBalancersFunc func(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTagsFunc          func(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
	DeleteLoadBalancerFunc    func(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error)
	DescribeTargetGroupsFunc  func(ctx context.Context, params *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
	DeleteTargetGroupFunc     func(ctx context.Context, params *elbv2.DeleteTargetGroupInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error)
}

func (m *mockELBv2Client) DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	if m.DescribeLoadBalancersFunc != nil {
		return m.DescribeLoadBalancersFunc(ctx, params, optFns...)
	}
	return &elbv2.DescribeLoadBalancersOutput{}, nil
}

func (m *mockELBv2Client) DescribeTags(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
	if m.DescribeTagsFunc != nil {
		return m.DescribeTagsFunc(ctx, params, optFns...)
	}
	return &elbv2.DescribeTagsOutput{}, nil
}

func (m *mockELBv2Client) DeleteLoadBalancer(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error) {
	if m.DeleteLoadBalancerFunc != nil {
		return m.DeleteLoadBalancerFunc(ctx, params, optFns...)
	}
	return &elbv2.DeleteLoadBalancerOutput{}, nil
}

func (m *mockELBv2Client) DescribeTargetGroups(ctx context.Context, params *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	if m.DescribeTargetGroupsFunc != nil {
		return m.DescribeTargetGroupsFunc(ctx, params, optFns...)
	}
	return &elbv2.DescribeTargetGroupsOutput{}, nil
}

func (m *mockELBv2Client) DeleteTargetGroup(ctx context.Context, params *elbv2.DeleteTargetGroupInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error) {
	if m.DeleteTargetGroupFunc != nil {
		return m.DeleteTargetGroupFunc(ctx, params, optFns...)
	}
	return &elbv2.DeleteTargetGroupOutput{}, nil
}

func TestCleanupELBv2LoadBalancers(t *testing.T) {
	clusterTag := "elbv2.k8s.aws/cluster"
	clusterName := "test-cluster"

	tests := []struct {
		name         string
		lbs          []elbv2types.LoadBalancer
		tagsByARN    map[string][]elbv2types.Tag
		deleteErrors map[string]error
		wantDeleted  []string
		wantErr      bool
	}{
		{
			name: "deletes only tagged NLBs",
			lbs: []elbv2types.LoadBalancer{
				{LoadBalancerArn: aws.String("arn:aws:elbv2::nlb-a"), LoadBalancerName: aws.String("a")},
				{LoadBalancerArn: aws.String("arn:aws:elbv2::nlb-b"), LoadBalancerName: aws.String("b")},
			},
			tagsByARN: map[string][]elbv2types.Tag{
				"arn:aws:elbv2::nlb-a": {{Key: aws.String(clusterTag), Value: aws.String(clusterName)}},
				"arn:aws:elbv2::nlb-b": {{Key: aws.String("other"), Value: aws.String("x")}},
			},
			wantDeleted: []string{"arn:aws:elbv2::nlb-a"},
		},
		{
			name:        "no load balancers returns no error",
			lbs:         nil,
			wantDeleted: nil,
		},
		{
			name: "wrong cluster tag value is ignored",
			lbs:  []elbv2types.LoadBalancer{{LoadBalancerArn: aws.String("arn:aws:elbv2::nlb-c"), LoadBalancerName: aws.String("c")}},
			tagsByARN: map[string][]elbv2types.Tag{
				"arn:aws:elbv2::nlb-c": {{Key: aws.String(clusterTag), Value: aws.String("different-cluster")}},
			},
			wantDeleted: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deleted []string
			mock := &mockELBv2Client{
				DescribeLoadBalancersFunc: func(ctx context.Context, p *elbv2.DescribeLoadBalancersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
					// When the waiter calls DescribeLoadBalancers with specific ARNs after
					// deletion, return LoadBalancerNotFound so the waiter treats it as success.
					if len(p.LoadBalancerArns) > 0 {
						allDeleted := true
						for _, arn := range p.LoadBalancerArns {
							if !slices.Contains(deleted, arn) {
								allDeleted = false
								break
							}
						}
						if allDeleted {
							return nil, &mockAPIError{code: "LoadBalancerNotFound", message: "not found"}
						}
					}
					return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: tt.lbs}, nil
				},
				DescribeTagsFunc: func(ctx context.Context, p *elbv2.DescribeTagsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
					var out []elbv2types.TagDescription
					for _, arn := range p.ResourceArns {
						out = append(out, elbv2types.TagDescription{
							ResourceArn: aws.String(arn),
							Tags:        tt.tagsByARN[arn],
						})
					}
					return &elbv2.DescribeTagsOutput{TagDescriptions: out}, nil
				},
				DeleteLoadBalancerFunc: func(ctx context.Context, p *elbv2.DeleteLoadBalancerInput, _ ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error) {
					deleted = append(deleted, *p.LoadBalancerArn)
					if err, ok := tt.deleteErrors[*p.LoadBalancerArn]; ok {
						return nil, err
					}
					return &elbv2.DeleteLoadBalancerOutput{}, nil
				},
			}

			count, err := cleanupELBv2LoadBalancers(context.Background(), mock, clusterName)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != len(tt.wantDeleted) {
				t.Errorf("deleted count = %d, want %d", count, len(tt.wantDeleted))
			}
			if len(deleted) != len(tt.wantDeleted) {
				t.Errorf("DeleteLoadBalancer called %d times, want %d", len(deleted), len(tt.wantDeleted))
			}
			for i, arn := range tt.wantDeleted {
				if i >= len(deleted) || deleted[i] != arn {
					t.Errorf("deleted[%d] = %v, want %v", i, deleted, tt.wantDeleted)
				}
			}
		})
	}
}

func TestCleanupKubernetesLoadBalancers(t *testing.T) {
	clusterName := "my-cluster"
	tagKey := "kubernetes.io/cluster/" + clusterName

	t.Run("deletes ELBs with matching cluster tag and cleans up SGs", func(t *testing.T) {
		var deletedELBs []string
		var deletedSGs []string

		elbMock := &mockELBClient{
			DescribeLoadBalancersFunc: func(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
				// Return ELBs on first call (paginator)
				return &elb.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
						{LoadBalancerName: aws.String("k8s-elb-abc123")},
						{LoadBalancerName: aws.String("other-elb")},
					},
				}, nil
			},
			DescribeTagsFunc: func(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error) {
				return &elb.DescribeTagsOutput{
					TagDescriptions: []elbtypes.TagDescription{
						{
							LoadBalancerName: aws.String("k8s-elb-abc123"),
							Tags: []elbtypes.Tag{
								{Key: aws.String(tagKey), Value: aws.String("owned")},
							},
						},
						{
							LoadBalancerName: aws.String("other-elb"),
							Tags: []elbtypes.Tag{
								{Key: aws.String("some-other-tag"), Value: aws.String("value")},
							},
						},
					},
				}, nil
			},
			DeleteLoadBalancerFunc: func(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error) {
				deletedELBs = append(deletedELBs, *params.LoadBalancerName)
				return &elb.DeleteLoadBalancerOutput{}, nil
			},
		}

		ec2Mock := &mockEC2Client{
			DescribeSecurityGroupsFunc: func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
				for _, f := range params.Filters {
					if *f.Name == "group-name" {
						return &ec2.DescribeSecurityGroupsOutput{
							SecurityGroups: []ec2types.SecurityGroup{
								{
									GroupId:   aws.String("sg-elb"),
									GroupName: aws.String("k8s-elb-abc123"),
									Tags: []ec2types.Tag{
										{Key: aws.String(tagKey), Value: aws.String("owned")},
									},
								},
							},
						}, nil
					}
				}
				return &ec2.DescribeSecurityGroupsOutput{}, nil
			},
			DeleteSecurityGroupFunc: func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
				deletedSGs = append(deletedSGs, *params.GroupId)
				return &ec2.DeleteSecurityGroupOutput{}, nil
			},
		}

		err := cleanupKubernetesLoadBalancers(context.Background(), elbMock, ec2Mock, clusterName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(deletedELBs) != 1 || deletedELBs[0] != "k8s-elb-abc123" {
			t.Errorf("expected k8s-elb-abc123 to be deleted, got %v", deletedELBs)
		}
		if len(deletedSGs) != 1 || deletedSGs[0] != "sg-elb" {
			t.Errorf("expected sg-elb to be deleted, got %v", deletedSGs)
		}
	})

	t.Run("leaves ELBs without cluster tag alone", func(t *testing.T) {
		elbMock := &mockELBClient{
			DescribeLoadBalancersFunc: func(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
				return &elb.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
						{LoadBalancerName: aws.String("unrelated-elb")},
					},
				}, nil
			},
			DescribeTagsFunc: func(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error) {
				return &elb.DescribeTagsOutput{
					TagDescriptions: []elbtypes.TagDescription{
						{
							LoadBalancerName: aws.String("unrelated-elb"),
							Tags: []elbtypes.Tag{
								{Key: aws.String("kubernetes.io/cluster/other-cluster"), Value: aws.String("owned")},
							},
						},
					},
				}, nil
			},
			DeleteLoadBalancerFunc: func(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error) {
				t.Fatal("DeleteLoadBalancer should not be called")
				return nil, nil
			},
		}

		ec2Mock := &mockEC2Client{}

		err := cleanupKubernetesLoadBalancers(context.Background(), elbMock, ec2Mock, clusterName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("handles empty paginator with no ELBs", func(t *testing.T) {
		elbMock := &mockELBClient{
			DescribeLoadBalancersFunc: func(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
				return &elb.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{},
				}, nil
			},
		}

		ec2Mock := &mockEC2Client{}

		err := cleanupKubernetesLoadBalancers(context.Background(), elbMock, ec2Mock, clusterName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("batches DescribeTags calls for more than 20 ELBs", func(t *testing.T) {
		// Create 25 ELBs to test batching
		var elbNames []string
		for i := range 25 {
			elbNames = append(elbNames, fmt.Sprintf("elb-%d", i))
		}

		var describedBatches [][]string
		elbMock := &mockELBClient{
			DescribeLoadBalancersFunc: func(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
				var descs []elbtypes.LoadBalancerDescription
				for _, name := range elbNames {
					descs = append(descs, elbtypes.LoadBalancerDescription{LoadBalancerName: aws.String(name)})
				}
				return &elb.DescribeLoadBalancersOutput{LoadBalancerDescriptions: descs}, nil
			},
			DescribeTagsFunc: func(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error) {
				describedBatches = append(describedBatches, params.LoadBalancerNames)
				// Return empty tags (no cluster tags)
				var tagDescs []elbtypes.TagDescription
				for _, name := range params.LoadBalancerNames {
					tagDescs = append(tagDescs, elbtypes.TagDescription{
						LoadBalancerName: aws.String(name),
						Tags:             []elbtypes.Tag{},
					})
				}
				return &elb.DescribeTagsOutput{TagDescriptions: tagDescs}, nil
			},
		}

		ec2Mock := &mockEC2Client{}

		err := cleanupKubernetesLoadBalancers(context.Background(), elbMock, ec2Mock, clusterName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify batching: should have 2 batches (20 + 5)
		if len(describedBatches) != 2 {
			t.Errorf("expected 2 DescribeTags batches, got %d", len(describedBatches))
		}
		if len(describedBatches) >= 1 && len(describedBatches[0]) != 20 {
			t.Errorf("expected first batch to have 20 ELBs, got %d", len(describedBatches[0]))
		}
		if len(describedBatches) >= 2 && len(describedBatches[1]) != 5 {
			t.Errorf("expected second batch to have 5 ELBs, got %d", len(describedBatches[1]))
		}
	})
}

func TestCleanupK8sSecurityGroupsByPrefix(t *testing.T) {
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

		deleted, err := cleanupK8sSecurityGroupsByPrefix(context.Background(), mock, tagKey, "k8s-elb-")
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

		deleted, err := cleanupK8sSecurityGroupsByPrefix(context.Background(), mock, tagKey, "k8s-elb-")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deleted, got %d", deleted)
		}
	})

	t.Run("no security groups found", func(t *testing.T) {
		mock := &mockEC2Client{}

		deleted, err := cleanupK8sSecurityGroupsByPrefix(context.Background(), mock, tagKey, "k8s-elb-")
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

func TestCleanupELBv2TargetGroups(t *testing.T) {
	clusterTag := "elbv2.k8s.aws/cluster"
	clusterName := "test-cluster"

	tgs := []elbv2types.TargetGroup{
		{TargetGroupArn: aws.String("arn:aws:elbv2::tg-a"), TargetGroupName: aws.String("a")},
		{TargetGroupArn: aws.String("arn:aws:elbv2::tg-b"), TargetGroupName: aws.String("b")},
	}
	tagsByARN := map[string][]elbv2types.Tag{
		"arn:aws:elbv2::tg-a": {{Key: aws.String(clusterTag), Value: aws.String(clusterName)}},
		"arn:aws:elbv2::tg-b": {{Key: aws.String("other"), Value: aws.String("x")}},
	}

	var deleted []string
	mock := &mockELBv2Client{
		DescribeTargetGroupsFunc: func(ctx context.Context, p *elbv2.DescribeTargetGroupsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
			return &elbv2.DescribeTargetGroupsOutput{TargetGroups: tgs}, nil
		},
		DescribeTagsFunc: func(ctx context.Context, p *elbv2.DescribeTagsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
			var out []elbv2types.TagDescription
			for _, arn := range p.ResourceArns {
				out = append(out, elbv2types.TagDescription{ResourceArn: aws.String(arn), Tags: tagsByARN[arn]})
			}
			return &elbv2.DescribeTagsOutput{TagDescriptions: out}, nil
		},
		DeleteTargetGroupFunc: func(ctx context.Context, p *elbv2.DeleteTargetGroupInput, _ ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error) {
			deleted = append(deleted, *p.TargetGroupArn)
			return &elbv2.DeleteTargetGroupOutput{}, nil
		},
	}

	count, err := cleanupELBv2TargetGroups(context.Background(), mock, clusterName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("deleted count = %d, want 1", count)
	}
	if len(deleted) != 1 || deleted[0] != "arn:aws:elbv2::tg-a" {
		t.Errorf("deleted = %v, want [arn:aws:elbv2::tg-a]", deleted)
	}
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
		defer cancel()
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
