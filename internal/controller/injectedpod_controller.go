package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	annotationInjected    = "sidecar-injector.webhook-system/injected"
	annotationSidecarName = "sidecar-injector.webhook-system/sidecar-name"
	annotationStatus      = "sidecar-injector.webhook-system/status"
	sidecarContainerName  = "envoy-sidecar"

	statusHealthy  = "healthy"
	statusDegraded = "degraded"
	statusUnknown  = "unknown"
)

// InjectedPodReconciler watches pods that have been injected by the mutating
// webhook and reconciles their sidecar state. If the sidecar container goes
// missing or becomes unhealthy, the controller updates the pod annotation
// and emits a warning event so operators are alerted without having to poll.
type InjectedPodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *InjectedPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if errors.IsNotFound(err) {
			// Pod deleted — nothing to reconcile.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get pod: %w", err)
	}

	// Only process pods that were injected by our webhook.
	if pod.Annotations[annotationInjected] != "true" {
		return ctrl.Result{}, nil
	}

	log.Info("reconciling injected pod", "pod", req.NamespacedName)

	status := r.reconcileSidecar(ctx, &pod)

	// Patch the status annotation if it changed.
	if pod.Annotations[annotationStatus] != status {
		patch := client.MergeFrom(pod.DeepCopy())
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations[annotationStatus] = status
		pod.Annotations[annotationSidecarName] = sidecarContainerName

		if err := r.Patch(ctx, &pod, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("patch pod annotations: %w", err)
		}
		log.Info("updated sidecar status", "pod", req.NamespacedName, "status", status)
	}

	return ctrl.Result{}, nil
}

// reconcileSidecar inspects the pod's container statuses and determines
// whether the injected sidecar is present and running.
func (r *InjectedPodReconciler) reconcileSidecar(ctx context.Context, pod *corev1.Pod) string {
	log := log.FromContext(ctx)

	// First check the spec — if the container isn't in the spec at all,
	// something removed it and we should surface that immediately.
	found := false
	for _, c := range pod.Spec.Containers {
		if c.Name == sidecarContainerName {
			found = true
			break
		}
	}
	if !found {
		log.Error(
			fmt.Errorf("sidecar container missing from spec"),
			"injected sidecar was removed from pod spec",
			"pod", pod.Name,
			"namespace", pod.Namespace,
		)
		r.emitWarningEvent(ctx, pod, "SidecarMissing",
			fmt.Sprintf("sidecar container %q was removed from pod spec", sidecarContainerName))
		return statusDegraded
	}

	// Pod not yet scheduled — status is not meaningful yet.
	if pod.Status.Phase == corev1.PodPending {
		return statusUnknown
	}

	// Walk container statuses for the sidecar.
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != sidecarContainerName {
			continue
		}

		if cs.Ready {
			return statusHealthy
		}

		// Container exists but isn't ready — inspect why.
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			log.Info("sidecar waiting", "pod", pod.Name, "reason", reason)
			if reason == "CrashLoopBackOff" || reason == "OOMKilled" {
				r.emitWarningEvent(ctx, pod, "SidecarUnhealthy",
					fmt.Sprintf("sidecar container is in %s", reason))
				return statusDegraded
			}
			return statusUnknown
		}

		if cs.State.Terminated != nil {
			r.emitWarningEvent(ctx, pod, "SidecarTerminated",
				fmt.Sprintf("sidecar container terminated with exit code %d",
					cs.State.Terminated.ExitCode))
			return statusDegraded
		}

		return statusUnknown
	}

	// Container is in the spec but not yet in status — still initialising.
	return statusUnknown
}

// emitWarningEvent creates a Warning event on the pod so it shows up in
// `kubectl describe pod` and in any alerting pipeline watching events.
func (r *InjectedPodReconciler) emitWarningEvent(ctx context.Context, pod *corev1.Pod, reason, message string) {
	log := log.FromContext(ctx)

	ev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", pod.Name),
			Namespace:    pod.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       pod.Name,
			Namespace:  pod.Namespace,
			UID:        pod.UID,
		},
		Reason:  reason,
		Message: message,
		Type:    corev1.EventTypeWarning,
		Source: corev1.EventSource{
			Component: "injected-pod-controller",
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Count:          1,
	}

	if err := r.Create(ctx, ev); err != nil {
		log.Error(err, "failed to emit warning event", "reason", reason)
	}
}

// SetupWithManager registers the controller with the manager and sets up
// a predicate so we only reconcile pods that carry the injected annotation.
// This avoids reconciling every pod in the cluster on every change.
func (r *InjectedPodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	injectedOnly := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetAnnotations()[annotationInjected] == "true"
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetAnnotations()[annotationInjected] == "true"
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false // nothing to reconcile on delete
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(injectedOnly).
		Complete(r)
}
