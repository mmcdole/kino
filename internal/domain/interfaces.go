package domain

// ListItem identifies an entity in a collection. Rendering, navigation, and
// sort presentation belong to the consumer, not the cache/backend contract.
type ListItem interface {
	GetID() string
	GetTitle() string
}
