package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func makePod(name string, annotations map[string]string, containers []corev1.Container, statuses []corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: statuses,
		},
	}
}

func TestReconcile_SkipsNonInjectedPods(t *testing.T) {
	pod := makePod("plain-pod", nil,
		[]corev1.Container{{Name: "app"}},
		nil,
	)

	r := &InjectedPodReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme()).WithObjects(pod).Build(),
		Scheme: scheme(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "plain-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Annotation should not have been set
	var updated corev1.Pod
	_ = r.Get(context.Background(), types.NamespacedName{Name: "plain-pod", Namespace: "default"}, &updated)
	if updated.Annotations[annotationStatus] != "" {
		t.Errorf("expected no status annotation on non-injected pod, got %q", updated.Annotations[annotationStatus])
	}
}

func TestReconcile_HealthySidecar(t *testing.T) {
	pod := makePod("injected-pod",
		map[string]string{annotationInjected: "true"},
		[]corev1.Container{{Name: "app"}, {Name: sidecarContainerName}},
		[]corev1.ContainerStatus{
			{Name: sidecarContainerName, Ready: true},
		},
	)

	r := &InjectedPodReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme()).WithObjects(pod).Build(),
		Scheme: scheme(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "injected-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated corev1.Pod
	_ = r.Get(context.Background(), types.NamespacedName{Name: "injected-pod", Namespace: "default"}, &updated)
	if updated.Annotations[annotationStatus] != statusHealthy {
		t.Errorf("expected status %q, got %q", statusHealthy, updated.Annotations[annotationStatus])
	}
}

func TestReconcile_MissingSidecarContainer(t *testing.T) {
	// Sidecar was removed from spec — only app container present
	pod := makePod("degraded-pod",
		map[string]string{annotationInjected: "true"},
		[]corev1.Container{{Name: "app"}}, // no sidecar
		nil,
	)

	r := &InjectedPodReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme()).WithObjects(pod).Build(),
		Scheme: scheme(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "degraded-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated corev1.Pod
	_ = r.Get(context.Background(), types.NamespacedName{Name: "degraded-pod", Namespace: "default"}, &updated)
	if updated.Annotations[annotationStatus] != statusDegraded {
		t.Errorf("expected status %q, got %q", statusDegraded, updated.Annotations[annotationStatus])
	}
}

func TestReconcile_CrashLoopingSidecar(t *testing.T) {
	pod := makePod("crashloop-pod",
		map[string]string{annotationInjected: "true"},
		[]corev1.Container{{Name: "app"}, {Name: sidecarContainerName}},
		[]corev1.ContainerStatus{
			{
				Name:  sidecarContainerName,
				Ready: false,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
			},
		},
	)

	r := &InjectedPodReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme()).WithObjects(pod).Build(),
		Scheme: scheme(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "crashloop-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated corev1.Pod
	_ = r.Get(context.Background(), types.NamespacedName{Name: "crashloop-pod", Namespace: "default"}, &updated)
	if updated.Annotations[annotationStatus] != statusDegraded {
		t.Errorf("expected status %q, got %q", statusDegraded, updated.Annotations[annotationStatus])
	}
}

func TestReconcile_NotFound(t *testing.T) {
	r := &InjectedPodReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme()).Build(),
		Scheme: scheme(),
	}

	// Should not error on missing pod — it was deleted
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "gone-pod", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error for missing pod, got: %v", err)
	}
}
