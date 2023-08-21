package controllers

import (
	"context"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

var globalLog = logf.Log

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
// +kubebuilder:rbac:groups=management.cattle.io,resources=clusters,verbs=*
// +kubebuilder:rbac:groups=management.cattle.io,resources=projects,verbs=update
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
		return r.deleteUserBindings(ctx, user.Username)
	}

	// Default roleTemplates
	roleTemplates := []struct {
		substring    string
		roleTemplate string
	}{
		{"cluster-admin", "cluster-admin"},
		{"cluster-auditor", "read-only"},
		{"developer", "projects-create"},
	}

	// Try to load from an external file, e.g., "roleTemplates.json"
	data, err := readFileIfExists("/config/roleTemplates.json")
	if err == nil {
		externalRoleTemplates, err := parseRoleTemplatesFile(data)
		if err == nil && len(externalRoleTemplates) > 0 {
			roleTemplates = externalRoleTemplates
		} else {
			globalLog.Error(err, "Failed to parse role templates file. Using defaults.")
		}
	} else {
		globalLog.Info("No external role templates file found. Using defaults.")
	}

	// Check the user's attributes or groups to decide which clusters they should have access to.
	clusters, err := determineClustersForUser(ctx, r, user)
	if err != nil {
		globalLog.Error(err, "Failed to retrieve list of clusters for user", "user", user.Name)
		return ctrl.Result{}, err
	}
	if len(user.PrincipalIDs) == 0 {
		globalLog.Info("User does not have yet any PrincipalIDs", "user", user.Name)
		return ctrl.Result{}, nil
	}
	for _, rt := range roleTemplates {
		if contains(user.Username, rt.substring) {
			for clusterName := range clusters {
				// Define a ClusterRoleTemplateBinding for each cluster the user should have access to.
				bindingName := user.Name + "-" + clusterName + "-" + rt.substring
				binding := &managementv3.ClusterRoleTemplateBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bindingName,
						Namespace: clusterName,
						Annotations: map[string]string{
							"created-by-pod": "rancher-operator-permissions-controller-manager",
						},
					},
					RoleTemplateName:  rt.roleTemplate,
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
							// Log error
							globalLog.Error(err, "Failed to create ClusterRoleTemplateBinding")
							return ctrl.Result{}, client.IgnoreNotFound(err)
						}
						// Log success
						globalLog.Info("Created ClusterRoleTemplateBinding", "Name", bindingName, "Namespace", clusterName)
					} else {
						// Log error
						globalLog.Error(err, "Failed to get ClusterRoleTemplateBinding")
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
							// Log error
							globalLog.Error(err, "Failed to update ClusterRoleTemplateBinding")
							return ctrl.Result{}, err
						}
						// Log success
						globalLog.Info("Updated ClusterRoleTemplateBinding", "Name", bindingName, "Namespace", clusterName)
					}
				}

			}
		} else {
			globalLog.Info("Failed to find role name in field user.Username", "user.Username", user.Username)
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

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
