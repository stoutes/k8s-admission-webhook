package mutate

import (
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"go.uber.org/zap/zaptest"
)

func podRequest(t *testing.T, pod corev1.Pod) *admissionv1.AdmissionRequest {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal pod: %v", err)
	}
	return &admissionv1.AdmissionRequest{
		UID:    "test-uid",
		Object: runtime.RawExtension{Raw: raw},
	}
}

func TestMutate_InjectsSidecar(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx"},
			},
		},
	}

	log := zaptest.NewLogger(t)
	resp := mutate(log, podRequest(t, pod))

	if !resp.Allowed {
		t.Fatalf("expected allowed, got denied: %s", resp.Result.Message)
	}
	if len(resp.Patch) == 0 {
		t.Fatal("expected patch, got none")
	}

	var patches []map[string]interface{}
	if err := json.Unmarshal(resp.Patch, &patches); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}

	found := false
	for _, p := range patches {
		if p["path"] == "/spec/containers/-" {
			found = true
		}
	}
	if !found {
		t.Error("expected container injection patch not found")
	}
}

func TestMutate_OptOut(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "opted-out",
			Namespace: "default",
			Annotations: map[string]string{
				annotationInject: "false",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
	}

	log := zaptest.NewLogger(t)
	resp := mutate(log, podRequest(t, pod))

	if !resp.Allowed {
		t.Fatal("expected allowed for opted-out pod")
	}
	if len(resp.Patch) > 0 {
		t.Error("expected no patch for opted-out pod")
	}
}

func TestMutate_Idempotent(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "already-injected",
			Namespace: "default",
			Annotations: map[string]string{
				annotationInjected: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
	}

	log := zaptest.NewLogger(t)
	resp := mutate(log, podRequest(t, pod))

	if !resp.Allowed {
		t.Fatal("expected allowed for already-injected pod")
	}
	if len(resp.Patch) > 0 {
		t.Error("expected no patch for already-injected pod")
	}
}
