package main

import "time"

// renderItem carries display-friendly times for email/API rendering.
type renderItem struct {
	*Item
	DisplayPublishedAt string
	DisplayUpdatedAt   string
}

type sourceData struct {
	Name  string
	Items []*renderItem
}

func buildSourceOrder(subscribers []Subscriber) map[Email][]string {
	sourceOrder := make(map[Email][]string, len(subscribers))
	for _, subscriber := range subscribers {
		names := make([]string, 0, len(subscriber.Sources))
		for _, source := range subscriber.Sources {
			names = append(names, source.Name)
		}
		sourceOrder[Email(subscriber.Email)] = names
	}
	return sourceOrder
}

func buildNotificationSources(email Email, items Items, sourceOrder map[Email][]string, now time.Time) ([]sourceData, []*Item) {
	userItems, ok := items.UserItems(email)
	if !ok {
		return nil, nil
	}

	sourceNames := sourceOrder[email]
	if len(sourceNames) == 0 {
		sourceNames = userItems.names
	}

	sources := make([]sourceData, 0, len(sourceNames))
	var flattened []*Item

	for _, source := range sourceNames {
		sourceItems, ok := userItems.get(source)
		if !ok || len(sourceItems) == 0 {
			continue
		}

		renderItems := make([]*renderItem, 0, len(sourceItems))
		for _, item := range sourceItems {
			renderItems = append(renderItems, &renderItem{
				Item:               item,
				DisplayPublishedAt: formatDisplayTime(item.PublishedAt, now),
				DisplayUpdatedAt:   formatDisplayTime(item.UpdatedAt, now),
			})
		}

		sources = append(sources, sourceData{Name: source, Items: renderItems})
		flattened = append(flattened, sourceItems...)
	}

	if len(sources) == 0 {
		return nil, nil
	}
	return sources, flattened
}
