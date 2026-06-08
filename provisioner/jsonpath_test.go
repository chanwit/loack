package provisioner

import (
	"testing"

	ackcompare "github.com/aws-controllers-k8s/runtime/pkg/compare"
)

// fake* mirror the shape of a generated ACK resource object -- pointer fields
// with lowerCamel json tags, nested structs -- so jsonPathOf can be unit-tested
// without linking a real controller. (The engine reflects on the Go type only;
// it never calls methods, so a plain struct is enough.)
type fakePayment struct {
	Payer *string `json:"payer,omitempty"`
}

type fakeSpec struct {
	ACL            *string      `json:"acl,omitempty"`
	Name           *string      `json:"name,omitempty"`
	RequestPayment *fakePayment `json:"requestPayment,omitempty"`
}

type fakeResource struct {
	Spec fakeSpec `json:"spec,omitempty"`
}

func TestJSONPath(t *testing.T) {
	obj := &fakeResource{}
	cases := map[string]string{
		"Spec.ACL":                  "spec.acl",
		"Spec.RequestPayment.Payer": "spec.requestPayment.payer",
		"Spec.Name":                 "spec.name",
	}
	for goPath, want := range cases {
		got := jsonPathOf(obj, ackcompare.NewPath(goPath))
		if got != want {
			t.Errorf("jsonPathOf(%q) = %q, want %q", goPath, got, want)
		}
	}
}
