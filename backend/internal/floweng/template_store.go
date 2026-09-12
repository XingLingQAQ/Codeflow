package floweng

import "fmt"

// TemplateStore is implemented by durable flow stores that can persist custom
// templates alongside flow instances.
type TemplateStore interface {
	PutTemplate(def CustomTemplate) error
	ListTemplateDefinitions() ([]CustomTemplate, error)
	DeleteTemplate(id TemplateID) error
}

func currentTemplateStore() TemplateStore {
	e, ok := GetEngine().(*InMemoryEngine)
	if !ok {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	store, _ := e.store.(TemplateStore)
	return store
}

// SaveTemplate validates, persists, and publishes a reusable custom template.
// In-memory engines still support the same API for tests and ephemeral use.
func SaveTemplate(def CustomTemplate) error {
	if def.ID == "" {
		return fmt.Errorf("template id is required")
	}
	if _, isBuiltin := builtinTemplates[def.ID]; isBuiltin {
		return fmt.Errorf("cannot override built-in template %s", def.ID)
	}
	td := def.toTemplateDef()
	if err := validateTemplateDef(td); err != nil {
		return err
	}
	if store := currentTemplateStore(); store != nil {
		if err := store.PutTemplate(def); err != nil {
			return err
		}
	}
	customMu.Lock()
	customTemplates[def.ID] = td
	customMu.Unlock()
	return nil
}

// DeleteTemplate removes a custom template from durable storage and the live
// registry. Built-in templates remain protected.
func DeleteTemplate(id TemplateID) error {
	if _, isBuiltin := builtinTemplates[id]; isBuiltin {
		return fmt.Errorf("cannot unregister built-in template %s", id)
	}
	customMu.RLock()
	_, ok := customTemplates[id]
	customMu.RUnlock()
	if !ok {
		return fmt.Errorf("template not found: %s", id)
	}
	if store := currentTemplateStore(); store != nil {
		if err := store.DeleteTemplate(id); err != nil {
			return err
		}
	}
	customMu.Lock()
	delete(customTemplates, id)
	customMu.Unlock()
	return nil
}
