package admission

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

type PatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Decode reads and deserializes an AdmissionReview from the request body.
func Decode(r *http.Request) (*admissionv1.AdmissionReview, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var review admissionv1.AdmissionReview
	if _, _, err = codecs.UniversalDeserializer().Decode(body, nil, &review); err != nil {
		return nil, fmt.Errorf("decode admission review: %w", err)
	}
	return &review, nil
}

// Encode writes the AdmissionReview response as JSON.
func Encode(w http.ResponseWriter, review *admissionv1.AdmissionReview) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(review)
}

// Allow returns a permissive AdmissionResponse with an optional message.
func Allow(msg string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Result:  &metav1.Status{Message: msg},
	}
}

// Deny returns a rejecting AdmissionResponse with a reason message.
func Deny(msg string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result:  &metav1.Status{Message: msg, Code: 403},
	}
}

// Err returns a rejecting AdmissionResponse signalling a processing error.
func Err(msg string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result:  &metav1.Status{Message: msg, Code: 400},
	}
}

// PatchResponse builds an AdmissionResponse carrying a JSON patch.
func PatchResponse(patches []PatchOp) (*admissionv1.AdmissionResponse, error) {
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, fmt.Errorf("marshal patch: %w", err)
	}
	pt := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &pt,
	}, nil
}
