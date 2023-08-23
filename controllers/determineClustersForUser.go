package controllers

import (
	"context"
	"strings"

	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
)

func determineClustersForUser(ctx context.Context, r *ClusterAssignmentReconciler, user *managementv3.User) (map[string]string, error) {
	var clusterList managementv3.ClusterList
	if err := r.List(ctx, &clusterList); err != nil {
		return nil, err
	}

	clusters := make(map[string]string)
	username := user.Username
	for _, cluster := range clusterList.Items {
		labels := cluster.ObjectMeta.GetLabels()
		if ownerLabel, ok := labels["owner"]; ok {
			// Check all substrings of length 5
			for i := 0; i <= len(username)-5; i++ {
				substr := username[i : i+5]
				if strings.Contains(ownerLabel, substr) {
					clusters[cluster.Name] = cluster.Namespace
					globalLog.V(1).Info("Matching cluster found", "cluster", cluster.Name, "display name ", cluster.Spec.DisplayName)
					break
				}
			}
		}
	}

	return clusters, nil
}
