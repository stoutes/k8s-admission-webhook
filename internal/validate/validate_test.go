package validate

import (
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"go.uber.org/zap/zaptest"
)

func deployRequest(t *testing.T, deploy appsv1.Deployment) *admissionv1.AdmissionRequest {
	t.Helper()
	raw, err := json.Marshal(deploy)
	if err != nil {
		t.Fatalf("marshal deployment: %v", err)
	}
	return &admissionv1.AdmissionRequest{
		UID:    "test-uid",
		Object: runtime.RawExtension{Raw: raw},
	}
}

func withLimits() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

func TestValidate_PassesWithLimits(t *testing.T) {
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx", Resources: withLimits()},
					},
				},
			},
		},
	}

	log := zaptest.NewLogger(t)
	resp := validate(log, deployRequest(t, deploy))
	if !resp.Allowed {
		t.Fatalf("expected allowed, got: %s", resp.Result.Message)
	}
}

func TestValidate_DeniesNoLimits(t *testing.T) {
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx"},
					},
				},
			},
		},
	}

	log := zaptest.NewLogger(t)
	resp := validate(log, deployRequest(t, deploy))
	if resp.Allowed {
		t.Fatal("expected denied for container without resource limits")
	}
}

func TestValidate_DeniesPartialLimits(t *testing.T) {
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									// CPU limit only — no memory limit
									corev1.ResourceCPU: resource.MustParse("500m"),
								},
							},
						},
					},
				},
			},
		},
	}

	log := zaptest.NewLogger(t)
	resp := validate(log, deployRequest(t, deploy))
	if resp.Allowed {
		t.Fatal("expected denied for container missing memory limit")
	}
}
