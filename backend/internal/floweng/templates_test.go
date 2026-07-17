package floweng

import "testing"

func TestTemplateLoopsReferenceExistingStages(t *testing.T) {
	for _, id := range ListTemplates() {
		info, ok := DescribeTemplate(id)
		if !ok {
			t.Fatalf("missing template %s", id)
		}
		types := map[StageType]bool{}
		for _, st := range info.Stages {
			types[st.Type] = true
		}
		for _, loop := range info.Loops {
			if !types[loop.From] {
				t.Errorf("template %s loop from %s missing stage", id, loop.From)
			}
			if !types[loop.To] {
				t.Errorf("template %s loop to %s missing stage", id, loop.To)
			}
		}
	}
}
