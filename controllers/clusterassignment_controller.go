package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterAssignmentReconciler reconciles a ClusterAssignment object
type ClusterAssignmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=permissions.xddevelopment.com,resources=clusterassignments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=permissions.xddevelopment.com,resources=clusterassignments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=permissions.xddevelopment.com,resources=clusterassignments/finalizers,verbs=update
//+kubebuilder:rbac:groups=management.cattle.io,resources=users,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=management.cattle.io,resources=clusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=management.cattle.io,resources=clusterroletemplatebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=*
// +kubebuilder:rbac:groups=management.cattle.io,resources=clusters,verbs=own
// +kubebuilder:rbac:groups=management.cattle.io,resources=projects,verbs=updatepsa
// +kubebuilder:rbac:groups=provisioning.cattle.io,resources=clusters,verbs=*
// +kubebuilder:rbac:groups=rke-machine-config.cattle.io,resources=*,verbs=*
// +kubebuilder:rbac:groups=rke-machine.cattle.io,resources=*,verbs=*
// +kubebuilder:rbac:groups=rke.cattle.io,resources=etcdsnapshots,verbs=get;list;watch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*
// +kubebuilder:rbac:urls=*,verbs=*

func (r *ClusterAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the User instance
	user := &managementv3.User{}
	err := r.Get(ctx, req.NamespacedName, user)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The user has been deleted. Nothing left to do.
			return ctrl.Result{}, nil
		}
		// handle error
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the user is being deleted
	if user.DeletionTimestamp != nil {
		// The user is being deleted
		return r.deleteUserBindings(ctx, user.Name)
	}

	// Check the user's attributes or groups to decide which clusters they should have access to.
	clusters, err := determineClustersForUser(ctx, r, user)
	if err != nil {
		return ctrl.Result{}, err
	}

	for clusterName := range clusters {
		// Define a ClusterRoleTemplateBinding for each cluster the user should have access to.
		bindingName := user.Name + "-" + clusterName + "-operator"
		binding := &managementv3.ClusterRoleTemplateBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: clusterName,
				Annotations: map[string]string{
					"created-by-pod": "rancher-operator-permissions-controller-manager",
				},
			},
			RoleTemplateName:  "cluster-owner",
			UserName:          user.Name,
			UserPrincipalName: user.PrincipalIDs[0],
			ClusterName:       clusterName,
		}

		// Check if ClusterRoleTemplateBinding already exists
		existingBinding := &managementv3.ClusterRoleTemplateBinding{}
		err := r.Get(ctx, client.ObjectKey{Namespace: binding.Namespace, Name: binding.Name}, existingBinding)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// ClusterRoleTemplateBinding does not exist, create it
				if err := r.Create(ctx, binding); err != nil {
					// handle error
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}
			} else {
				// handle error
				return ctrl.Result{}, err
			}
		} else {
			// ClusterRoleTemplateBinding exists, check if it needs to be updated
			if !reflect.DeepEqual(existingBinding.RoleTemplateName, binding.RoleTemplateName) ||
				!reflect.DeepEqual(existingBinding.UserName, binding.UserName) ||
				!reflect.DeepEqual(existingBinding.UserPrincipalName, binding.UserPrincipalName) ||
				!reflect.DeepEqual(existingBinding.ClusterName, binding.ClusterName) {

				// Update existing ClusterRoleTemplateBinding
				existingBinding.RoleTemplateName = binding.RoleTemplateName
				existingBinding.UserName = binding.UserName
				existingBinding.UserPrincipalName = binding.UserPrincipalName
				existingBinding.ClusterName = binding.ClusterName

				if err := r.Update(ctx, existingBinding); err != nil {
					// handle error
					return ctrl.Result{}, err
				}
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *ClusterAssignmentReconciler) deleteUserBindings(ctx context.Context, userName string) (ctrl.Result, error) {
	var bindingList managementv3.ClusterRoleTemplateBindingList
	if err := r.List(ctx, &bindingList, client.MatchingFields{managementv3.ClusterRoleTemplateBinding{}.UserName: userName}); err != nil {
		return ctrl.Result{}, err
	}
	for _, binding := range bindingList.Items {
		if err := r.Delete(ctx, &binding); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAssignmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv3.User{}).
		Complete(r)
}

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
			// Check all substrings of length 4
			for i := 0; i <= len(username)-4; i++ {
				substr := username[i : i+4]
				if strings.Contains(ownerLabel, substr) {
					clusters[cluster.Name] = cluster.Namespace
					break
				}
			}
		}
	}

	return clusters, nil
}
