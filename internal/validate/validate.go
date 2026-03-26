package validate

import (
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"go.uber.org/zap"

	"github.com/yourorg/k8s-admission-webhook/internal/admission"
)

// Handler returns an http.HandlerFunc for the /validate endpoint.
func Handler(log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		review, err := admission.Decode(r)
		if err != nil {
			log.Error("decode failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		review.Response = validate(log, review.Request)
		review.Response.UID = review.Request.UID

		if err := admission.Encode(w, review); err != nil {
			log.Error("encode failed", zap.Error(err))
		}
	}
}

func validate(log *zap.Logger, req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	var deploy appsv1.Deployment
	if err := json.Unmarshal(req.Object.Raw, &deploy); err != nil {
		return admission.Err("unmarshal deployment: " + err.Error())
	}

	log.Info("validate request",
		zap.String("namespace", req.Namespace),
		zap.String("name", req.Name),
	)

	if err := enforceResourceLimits(deploy.Spec.Template.Spec.Containers); err != nil {
		log.Warn("validation denied", zap.String("deployment", req.Name), zap.Error(err))
		return admission.Deny(err.Error())
	}

	log.Info("validation passed", zap.String("deployment", req.Name))
	return admission.Allow("all containers have resource limits")
}

// enforceResourceLimits rejects deployments where any container is missing
// CPU or memory limits. This prevents unbounded resource consumption.
func enforceResourceLimits(containers []corev1.Container) error {
	for _, c := range containers {
		if c.Resources.Limits == nil {
			return fmt.Errorf("container %q has no resource limits set", c.Name)
		}
		if _, ok := c.Resources.Limits[corev1.ResourceCPU]; !ok {
			return fmt.Errorf("container %q missing CPU limit", c.Name)
		}
		if _, ok := c.Resources.Limits[corev1.ResourceMemory]; !ok {
			return fmt.Errorf("container %q missing memory limit", c.Name)
		}
	}
	return nil
}
