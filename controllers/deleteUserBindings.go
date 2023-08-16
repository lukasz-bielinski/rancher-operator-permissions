package controllers

import (
	"context"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ClusterAssignmentReconciler) deleteUserBindings(ctx context.Context, Username string) (ctrl.Result, error) {
	var bindingList managementv3.ClusterRoleTemplateBindingList
	globalLog.Info("Starting deleteUserBindings method...")

	// List all ClusterRoleTemplateBindings
	if err := r.List(ctx, &bindingList); err != nil {
		return ctrl.Result{}, err
	}

	// Filter out objects based on annotations and name
	toDelete := []*managementv3.ClusterRoleTemplateBinding{}
	for _, binding := range bindingList.Items {
		globalLog.Info("Checking binding", "UserName", binding.UserName, "Annotations", binding.Annotations)

		if binding.UserName == Username {
			annotationValue, hasAnnotation := binding.Annotations["created-by-pod"]
			if hasAnnotation && annotationValue == "rancher-operator-permissions-controller-manager" {
				toDelete = append(toDelete, &binding)
			} else if !hasAnnotation {
				globalLog.Info("Binding has no 'created-by-pod' annotation", "BindingName", binding.Name)
			} else {
				globalLog.Info("Binding annotation doesn't match expected value", "BindingName", binding.Name, "AnnotationValue", annotationValue)
			}
		} else {
			globalLog.Info("Binding UserName doesn't match the given UserName", "BindingUserName", binding.UserName, "GivenUserName", Username)
		}
	}

	globalLog.Info("Number of ClusterRoleTemplateBindings to delete", "count", len(toDelete))

	for _, binding := range toDelete {
		if err := r.Delete(ctx, binding); err != nil {
			globalLog.Info("Error deleting ClusterRoleTemplateBinding", "name", binding.Name, "namespace", binding.Namespace, "error", err)
			return ctrl.Result{}, err
		} else {
			globalLog.Info("Successfully deleted ClusterRoleTemplateBinding", "name", binding.Name, "namespace", binding.Namespace)
		}
	}

	globalLog.Info("Exiting deleteUserBindings method...")
	return ctrl.Result{}, nil
}
