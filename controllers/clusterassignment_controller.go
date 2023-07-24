//
//	func (r *ClusterAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
//		// Fetch the User instance
//		user := &managementv3.User{}
//		err := r.Get(ctx, req.NamespacedName, user)
//		if err != nil {
//			// handle error
//			return ctrl.Result{}, err
//		}
//
//		// Check the user's attributes or groups to decide which clusters they should have access to.
//		// This logic will vary greatly depending on your needs.
//		clusters, err := determineClustersForUser(ctx, r, user)
//		if err != nil {
//			return ctrl.Result{}, err
//		}
//
//		for _, cluster := range clusters {
//			// Create a ClusterRoleTemplateBinding for each cluster the user should have access to.
//			// Please check the structure of the managementv3.ClusterRoleTemplateBinding object
//			binding := &managementv3.ClusterRoleTemplateBinding{
//				ObjectMeta: metav1.ObjectMeta{
//					Name:      user.Name + "-" + cluster,
//					Namespace: "default",
//				},
//				// More fields might be required here
//			}
//
//			// Apply the ClusterRoleTemplateBinding to the cluster.
//			if err := r.Create(ctx, binding); err != nil {
//				// handle error
//				return ctrl.Result{}, err
//			}
//		}
//
//		return ctrl.Result{}, nil
//	}

/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

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

	// Secret name and namespace
	secretName := user.Name + "-test-secret"
	secretNamespace := "default"

	// Check if the user is being deleted
	if user.DeletionTimestamp != nil {
		// The user is being deleted
		if err := r.deleteUserSecret(ctx, secretName, secretNamespace); err != nil {
			// if fail to delete the external dependency here, return with error
			// so that it can be retried
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Check if the secret already exists
	secretExists := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: secretNamespace}, secretExists)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The secret doesn't exist, create it
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: secretNamespace,
				},
				StringData: map[string]string{
					"username": user.Name,
					// Add more data here
				},
			}

			if err := r.Create(ctx, secret); err != nil {
				// handle error
				return ctrl.Result{}, err
			}
		} else {
			// Some other error occurred when trying to fetch the Secret, requeue the request
			return ctrl.Result{}, err
		}
	}

	// Check the user's attributes or groups to decide which clusters they should have access to.
	clusters, err := determineClustersForUser(ctx, r, user)
	if err != nil {
		return ctrl.Result{}, err
	}

	for clusterName, namespace := range clusters {
		// Create a ClusterRoleTemplateBinding for each cluster the user should have access to.
		binding := &managementv3.ClusterRoleTemplateBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      user.Name + "-" + clusterName,
				Namespace: namespace,
			},
			RoleTemplateName:  "cluster-owner",
			UserName:          user.Name,
			UserPrincipalName: user.PrincipalIDs[0],
		}

		// Apply the ClusterRoleTemplateBinding to the cluster.
		if err := r.Create(ctx, binding); err != nil {
			// handle error
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	} // No error occurred
	return ctrl.Result{}, nil
}

func (r *ClusterAssignmentReconciler) deleteUserSecret(ctx context.Context, secretName, secretNamespace string) error {
	// delete secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
		},
	}
	err := r.Delete(ctx, secret)
	if err != nil {
		// it was removed already, so ignore this error
		if apierrors.IsNotFound(err) {
			return nil
		}
	}
	return err
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
	for _, cluster := range clusterList.Items {
		labels := cluster.ObjectMeta.GetLabels()
		if ownerLabel, ok := labels["owner"]; ok {
			if strings.Contains(ownerLabel, user.Username) {
				clusters[cluster.Name] = cluster.Namespace
			}
		}
	}

	return clusters, nil
}
