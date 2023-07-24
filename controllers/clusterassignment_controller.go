package controllers

import (
	"context"
	"strings"

	permissionsv1alpha1 "github.com/lukasz-bielinski/rancher-operator-permissions/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterAssignmentReconciler reconciles a ClusterAssignment object
type ClusterAssignmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the User instance
	user := &managementv3.User{}
	err := r.Get(ctx, req.NamespacedName, user)
	if err != nil {
		// handle error
		return ctrl.Result{}, err
	}

	// Check the user's attributes or groups to decide which clusters they should have access to.
	// This logic will vary greatly depending on your needs.
	clusters, err := determineClustersForUser(ctx, r, user)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, cluster := range clusters {
		// Create a ClusterRoleTemplateBinding for each cluster the user should have access to.
		// Please check the structure of the managementv3.ClusterRoleTemplateBinding object
		binding := &managementv3.ClusterRoleTemplateBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      user.Name + "-" + cluster,
				Namespace: "default",
			},
			// More fields might be required here
		}

		// Apply the ClusterRoleTemplateBinding to the cluster.
		if err := r.Create(ctx, binding); err != nil {
			// handle error
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAssignmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&permissionsv1alpha1.ClusterAssignment{}).
		Complete(r)
}

func determineClustersForUser(ctx context.Context, r *ClusterAssignmentReconciler, user *managementv3.User) ([]string, error) {
	// Find the user's group
	group := ""
	for _, principalId := range user.PrincipalIDs {
		if strings.Contains(principalId, "eks-dev-team-") {
			group = strings.Replace(principalId, "eks-", "", 1)
			break
		}
	}

	// If the user isn't in a relevant group, they don't get access to any clusters
	if group == "" {
		return nil, nil
	}

	// Fetch all clusters
	clusters := &managementv3.ClusterList{}
	if err := r.List(ctx, clusters); err != nil {
		return nil, err
	}

	// Filter the clusters based on their names
	result := []string{}
	for _, cluster := range clusters.Items {
		if strings.Contains(cluster.Name, group) {
			result = append(result, cluster.Name)
		}
	}

	return result, nil
}
