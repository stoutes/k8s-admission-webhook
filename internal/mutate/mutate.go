package mutate

import (
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"go.uber.org/zap"

	"github.com/yourorg/k8s-admission-webhook/internal/admission"
)

const (
	annotationInject  = "sidecar-injector.webhook-system/inject"
	annotationInjected = "sidecar-injector.webhook-system/injected"
)

var sidecarContainer = corev1.Container{
	Name:  "envoy-sidecar",
	Image: "envoyproxy/envoy:v1.29-latest",
	Ports: []corev1.ContainerPort{
		{Name: "envoy-admin", ContainerPort: 9901, Protocol: corev1.ProtocolTCP},
	},
	Resources: corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	},
	VolumeMounts: []corev1.VolumeMount{
		{Name: "envoy-config", MountPath: "/etc/envoy", ReadOnly: true},
	},
}

var sidecarVolume = corev1.Volume{
	Name: "envoy-config",
	VolumeSource: corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "envoy-config"},
		},
	},
}

// Handler returns an http.HandlerFunc for the /mutate endpoint.
func Handler(log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		review, err := admission.Decode(r)
		if err != nil {
			log.Error("decode failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		review.Response = mutate(log, review.Request)
		review.Response.UID = review.Request.UID

		if err := admission.Encode(w, review); err != nil {
			log.Error("encode failed", zap.Error(err))
		}
	}
}

func mutate(log *zap.Logger, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		return admission.Err("unmarshal pod: " + err.Error())
	}

	log.Info("mutate request",
		zap.String("namespace", req.Namespace),
		zap.String("name", req.Name),
	)

	// Opt-out: annotation explicitly disables injection
	if pod.Annotations[annotationInject] == "false" {
		log.Info("injection opted out", zap.String("pod", req.Name))
		return admission.Allow("injection opted out")
	}

	// Idempotency: skip if already injected
	if pod.Annotations[annotationInjected] == "true" {
		log.Info("already injected", zap.String("pod", req.Name))
		return admission.Allow("already injected")
	}

	patches := buildPatches(&pod)

	resp, err := admission.PatchResponse(patches)
	if err != nil {
		return admission.Err(err.Error())
	}

	log.Info("sidecar injected", zap.String("pod", req.Name), zap.Int("patches", len(patches)))
	return resp
}

func buildPatches(pod *corev1.Pod) []admission.PatchOp {
	var patches []admission.PatchOp

	// Ensure containers array exists
	if len(pod.Spec.Containers) == 0 {
		patches = append(patches, admission.PatchOp{
			Op: "add", Path: "/spec/containers", Value: []corev1.Container{},
		})
	}
	patches = append(patches, admission.PatchOp{
		Op:    "add",
		Path:  "/spec/containers/-",
		Value: sidecarContainer,
	})

	// Ensure volumes array exists
	if len(pod.Spec.Volumes) == 0 {
		patches = append(patches, admission.PatchOp{
			Op: "add", Path: "/spec/volumes", Value: []corev1.Volume{},
		})
	}
	patches = append(patches, admission.PatchOp{
		Op:    "add",
		Path:  "/spec/volumes/-",
		Value: sidecarVolume,
	})

	// Ensure annotations map exists
	if pod.Annotations == nil {
		patches = append(patches, admission.PatchOp{
			Op: "add", Path: "/metadata/annotations", Value: map[string]string{},
		})
	}
	// Mark as injected — note ~1 escaping for '/' in JSON Pointer
	patches = append(patches, admission.PatchOp{
		Op:    "add",
		Path:  "/metadata/annotations/sidecar-injector.webhook-system~1injected",
		Value: "true",
	})

	return patches
}
