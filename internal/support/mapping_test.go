package support

import (
	"testing"

	. "github.com/dubi-project/dubi-go/base"
)

func TestNullableMappingDoesNotRequireOrDefaultMissingFields(t *testing.T) {
	fields := Vars{"id": {Type: "int", Required: true}, "name": {Type: "string", Default: "default"}}
	data, res := Mapping(nil, fields, Map{}, true)
	if (res != nil && res.Fail()) || len(data) != 0 {
		t.Fatalf("partial mapping: %v %v", data, res)
	}
	data, res = Mapping(nil, fields, Map{"id": "7"}, true)
	if (res != nil && res.Fail()) || data["id"] != int64(7) {
		t.Fatalf("partial conversion: %v %v", data, res)
	}
	if !fields["id"].Required || fields["name"].Default != "default" {
		t.Fatal("mapping changed shared definitions")
	}
	if _, res := Mapping(nil, fields, Map{}, false); res == nil || res.OK() {
		t.Fatal("required mapping accepted empty payload")
	}
}
