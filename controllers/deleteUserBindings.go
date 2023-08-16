package controllers

import (
	"context"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ClusterAssignmentReconciler) deleteUserBindings(ctx context.Context, Name string) (ctrl.Result, error) {
	var bindingList managementv3.ClusterRoleTemplateBindingList

	// List all ClusterRoleTemplateBindings
	if err := r.List(ctx, &bindingList); err != nil {
		return ctrl.Result{}, err
	}

	// Filter out objects based on annotations and name
	toDelete := []*managementv3.ClusterRoleTemplateBinding{}
	for _, binding := range bindingList.Items {
		annotationValue, hasAnnotation := binding.Annotations["created-by-pod"]
		if hasAnnotation && annotationValue == "rancher-operator-permissions-controller-manager" {
			toDelete = append(toDelete, &binding)
		} else if !hasAnnotation && binding.Name == Name {
			toDelete = append(toDelete, &binding)
		}
	}

	// Now, we'll delete the filtered objects
	for _, binding := range toDelete {
		if err := r.Delete(ctx, binding); err != nil {
			globalLog.Info("Error deleting ClusterRoleTemplateBinding", "name", binding.Name, "namespace", binding.Namespace, "error", err)
			return ctrl.Result{}, err
		} else {
			globalLog.Info("Deleted ClusterRoleTemplateBinding", "name", binding.Name, "namespace", binding.Namespace)
		}
	}

	return ctrl.Result{}, nil
}
