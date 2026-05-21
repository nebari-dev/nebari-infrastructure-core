package azure

import (
	"context"
	"fmt"
)

func listSupportedVersions(ctx context.Context, api managedClusterVersionsAPI, location string) ([]string, error) {
	resp, err := api.ListKubernetesVersions(ctx, location, nil)
	if err != nil {
		return nil, fmt.Errorf("list AKS versions in %s: %w", location, err)
	}
	out := make([]string, 0, len(resp.Values))
	for _, v := range resp.Values {
		if v != nil && v.Version != nil {
			out = append(out, *v.Version)
		}
	}
	return out, nil
}
