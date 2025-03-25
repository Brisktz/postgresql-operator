/*
Copyright 2025.

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

package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"text/template"
	"time"

	dbv1 "example.com/postgresql/api/v1"
)

// PostgresqlReconciler reconciles a Postgresql object
type PostgresqlReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=db.example.com,resources=postgresqls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=db.example.com,resources=postgresqls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=db.example.com,resources=postgresqls/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Postgresql object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *PostgresqlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling postgresql")

	// 获取Postgresql对象
	logger.Info("Get postgresql object")
	pg := new(dbv1.Postgresql)
	if err := r.Client.Get(ctx, req.NamespacedName, pg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	objectMeta := pg.ObjectMeta.DeepCopy()
	objectMeta.ResourceVersion = ""

	// 设置Status开始时间
	if pg.Status.StartTime == nil {
		pg.Status.StartTime = &metav1.Time{Time: time.Now()}
	}

	// 初始化Conditions
	if pg.Status.Conditions == nil {
		pg.Status.Conditions = make([]metav1.Condition, 0, 4)
	}

	// 创建或更新Secret对象
	logger.Info("Get secret object")
	secret := new(corev1.Secret)
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Create Secret Object")
			newSecret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "v1",
				},
				ObjectMeta: *objectMeta,
				Type:       corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"postgres-password": []byte(pg.Spec.PostgresqlPassword),
				},
			}

			// 设置ControllerReference
			if err := ctrl.SetControllerReference(pg, newSecret, r.Scheme); err != nil {
				logger.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newSecret); err != nil {
				logger.Error(err, "Failed to create secret")
				return ctrl.Result{}, err
			}

			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlSecretReady, metav1.ConditionTrue, "CreateSecret", fmt.Sprintf("Secret %s%s 创建成功", newSecret.Namespace, newSecret.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}

		} else {
			logger.Error(err, "Failed to get Secret Object")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Update secret object")
		reason := "CreateSecret"
		encodeSecret := base64.StdEncoding.EncodeToString([]byte(pg.Spec.PostgresqlPassword))
		if string(secret.Data["postgres-password"]) != encodeSecret {
			secret.Data["postgres-password"] = []byte(pg.Spec.PostgresqlPassword)
			reason = "UpdateSecret"
			if err := r.Update(ctx, secret); err != nil {
				logger.Error(err, "Failed to update secret")
				return ctrl.Result{}, err
			}
			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlSecretReady, metav1.ConditionTrue, reason, fmt.Sprintf("Secret %s%s 更新成功", secret.Namespace, secret.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
		}
	}

	// 创建或更新Service Headless
	logger.Info("Get service headless Object")
	svcHeadless := new(corev1.Service)

	addAnnotation := objectMeta.DeepCopy()
	addAnnotation.ResourceVersion = ""
	addAnnotation.Annotations = map[string]string{
		"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
	}
	addAnnotation.Name = addAnnotation.Name + "-hl"
	if err := r.Get(ctx, req.NamespacedName, svcHeadless); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Create headless service")
			newSvcHeadless := &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: *addAnnotation,
				Spec: corev1.ServiceSpec{
					Type:                     corev1.ServiceTypeClusterIP,
					ClusterIP:                "None",
					PublishNotReadyAddresses: true,
					Ports: []corev1.ServicePort{
						{
							Name: "tcp-postgresql",
							Port: 5432,
							TargetPort: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "tcp-postgresql",
							},
						},
					},
					Selector: map[string]string{
						"app.kubernetes.io/instance":  objectMeta.Labels["app.kubernetes.io/instance"],
						"app.kubernetes.io/name":      objectMeta.Labels["app.kubernetes.io/name"],
						"app.kubernetes.io/component": objectMeta.Labels["app.kubernetes.io/component"],
					},
				},
			}

			if err := ctrl.SetControllerReference(pg, newSvcHeadless, r.Scheme); err != nil {
				logger.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newSvcHeadless); err != nil {
				logger.Error(err, "Failed to create headless service")
				return ctrl.Result{}, err
			}

			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlServiceHeadlessReady, metav1.ConditionTrue, "CreateServiceHl", fmt.Sprintf("Service Headless %s%s 创建成功", newSvcHeadless.Namespace, newSvcHeadless.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
		} else {
			logger.Error(err, "Failed to get Headless Service")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Update headless service")
		condition := newCondition(dbv1.PostgresqlServiceHeadlessReady, metav1.ConditionTrue, "CreateServiceHl", fmt.Sprintf("Service Headless %s%s 创建成功", svcHeadless.Namespace, svcHeadless.Name))
		pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
		if err := r.Status().Update(ctx, pg); err != nil {
			logger.Error(err, "Failed to update postgresql status")
			return ctrl.Result{}, err
		}
	}

	// 创建或更新Service
	logger.Info("Get service object")
	svc := new(corev1.Service)
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Create service")
			newSvc := &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: *objectMeta,
				Spec: corev1.ServiceSpec{
					Type:            corev1.ServiceTypeClusterIP,
					SessionAffinity: corev1.ServiceAffinityNone,
					Ports: []corev1.ServicePort{
						{
							Name: "tcp-postgresql",
							Port: 5432,
							TargetPort: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "tcp-postgresql",
							},
						},
					},
					Selector: map[string]string{
						"app.kubernetes.io/instance":  objectMeta.Labels["app.kubernetes.io/instance"],
						"app.kubernetes.io/name":      objectMeta.Labels["app.kubernetes.io/name"],
						"app.kubernetes.io/component": objectMeta.Labels["app.kubernetes.io/component"],
					},
				},
			}

			if err := ctrl.SetControllerReference(pg, newSvc, r.Scheme); err != nil {
				logger.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newSvc); err != nil {
				logger.Error(err, "Failed to create service")
				return ctrl.Result{}, err
			}

			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlServiceReady, metav1.ConditionTrue, "CreateService", fmt.Sprintf("Service %s%s 创建成功", newSvc.Namespace, newSvc.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
		} else {
			logger.Error(err, "Failed to get service")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Update service")
		condition := newCondition(dbv1.PostgresqlServiceReady, metav1.ConditionTrue, "CreateService", fmt.Sprintf("Service %s%s 创建成功", svc.Namespace, svc.Name))
		pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
		if err := r.Status().Update(ctx, pg); err != nil {
			logger.Error(err, "Failed to update postgresql status")
			return ctrl.Result{}, err
		}
	}
	// 创建或更新StatefulSet
	logger.Info("Get statefulSet object")
	sts := new(appsv1.StatefulSet)
	if err := r.Get(ctx, req.NamespacedName, sts); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Create statefulSet")
			tepl, err := template.ParseFiles("templates/statefulset.yaml")
			if err != nil {
				logger.Error(err, "Failed to parse template")
				return ctrl.Result{}, err
			}
			type stsTemplate struct {
				Name              string
				Namespace         string
				Labels            map[string]string
				PostgresqlVersion string
			}
			data := stsTemplate{
				Name:              objectMeta.Name,
				Namespace:         objectMeta.Namespace,
				Labels:            objectMeta.Labels,
				PostgresqlVersion: pg.Spec.PostgresVersion,
			}
			buf := new(bytes.Buffer)
			if err := tepl.Execute(buf, data); err != nil {
				logger.Error(err, "Failed to execute template")
				return ctrl.Result{}, err
			}
			decode := scheme.Codecs.UniversalDeserializer().Decode
			obj, _, err := decode(buf.Bytes(), nil, nil)
			// 强制转换
			newSts := obj.(*appsv1.StatefulSet)

			if err := ctrl.SetControllerReference(pg, newSts, r.Scheme); err != nil {
				logger.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, newSts); err != nil {
				logger.Error(err, "Failed to create statefulSet")
				return ctrl.Result{}, err
			}

			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlStatefulSetReady, metav1.ConditionUnknown, "CreateStatefulSet", fmt.Sprintf("StatefulSet %s%s 创建成功", newSts.Namespace, newSts.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
			// 重新进入Reconcile循环
		} else {
			logger.Error(err, "Failed to get StatefulSet")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Update statefulSet")
		if sts.Spec.Template.Spec.Containers[0].Image != "docker.io/bitnami/postgresql:"+pg.Spec.PostgresVersion {
			sts.Spec.Template.Spec.Containers[0].Image = "docker.io/bitnami/postgresql:" + pg.Spec.PostgresVersion
			if err := r.Update(ctx, sts); err != nil {
				logger.Error(err, "Failed to update statefulSet")
				return ctrl.Result{}, err
			}

			// 设置Status的Condition
			condition := newCondition(dbv1.PostgresqlStatefulSetReady, metav1.ConditionUnknown, "UpdateStatefulSet", fmt.Sprintf("StatefulSet %s%s 创建成功", sts.Namespace, sts.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
		}
		// 判断当前的statefulSet是否完成更新
		// 如果没有完成，重新进入循环
		if sts.Status.CurrentReplicas != sts.Status.Replicas {
			logger.Info("StatefulSet is updating")
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		} else {
			logger.Info("StatefulSet is updated completely")
			condition := newCondition(dbv1.PostgresqlStatefulSetReady, metav1.ConditionTrue, "CreateStatefulSet", fmt.Sprintf("StatefulSet %s%s 创建成功", sts.Namespace, sts.Name))
			pg.Status.Conditions = setConditions(pg.Status.Conditions, condition)
			if err := r.Status().Update(ctx, pg); err != nil {
				logger.Error(err, "Failed to update postgresql status")
				return ctrl.Result{}, err
			}
		}
	}

	logger.Info("Reconcile finished")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresqlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dbv1.Postgresql{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Secret{}).
		Named("postgresql").
		Complete(r)
}

func setConditions(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
	for i, c := range conditions {
		if c.Type == condition.Type {
			if c.Status != condition.Status || c.Reason != condition.Reason {
				conditions[i].LastTransitionTime = metav1.Now()
			} else {
				conditions[i].LastTransitionTime = c.LastTransitionTime
			}
			conditions[i] = condition
			return conditions
		}
	}
	conditions = append(conditions, condition)
	return conditions
}

func newCondition(conditionType string, statusValue metav1.ConditionStatus, reason, message string) metav1.Condition {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             statusValue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	return condition
}

//func (r *PostgresqlReconciler)dealWithSecret(ctx context.Context, req ctrl.Request, pg *dbv1.Postgresql, logger logr.Logger) error {
//
//}
