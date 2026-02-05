package argocd

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestApplicationGVR(t *testing.T) {
	// Verify the ApplicationGVR constant has correct values
	expected := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	if ApplicationGVR.Group != expected.Group {
		t.Errorf("ApplicationGVR.Group = %q, want %q", ApplicationGVR.Group, expected.Group)
	}
	if ApplicationGVR.Version != expected.Version {
		t.Errorf("ApplicationGVR.Version = %q, want %q", ApplicationGVR.Version, expected.Version)
	}
	if ApplicationGVR.Resource != expected.Resource {
		t.Errorf("ApplicationGVR.Resource = %q, want %q", ApplicationGVR.Resource, expected.Resource)
	}
}

func TestApplicationGVR_String(t *testing.T) {
	// Test that the GVR produces a sensible string representation
	str := ApplicationGVR.String()
	if str == "" {
		t.Error("ApplicationGVR.String() should not be empty")
	}
	// Verify the string contains expected components
	if !strings.Contains(str, "argoproj.io") {
		t.Errorf("ApplicationGVR.String() = %q, should contain 'argoproj.io'", str)
	}
	if !strings.Contains(str, "v1alpha1") {
		t.Errorf("ApplicationGVR.String() = %q, should contain 'v1alpha1'", str)
	}
	if !strings.Contains(str, "applications") {
		t.Errorf("ApplicationGVR.String() = %q, should contain 'applications'", str)
	}
}
