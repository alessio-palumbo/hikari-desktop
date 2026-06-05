package backend

import (
	"sort"
	"strings"
)

func sortDeviceSnapshot(snapshot DeviceSnapshot) DeviceSnapshot {
	locationNames := make(map[string]string, len(snapshot.Locations))
	for _, location := range snapshot.Locations {
		locationNames[location.ID] = location.Name
	}

	groupNames := make(map[string]string, len(snapshot.Groups))
	groupLocationIDs := make(map[string]string, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		groupNames[group.ID] = group.Name
		groupLocationIDs[group.ID] = group.LocationID
	}

	sort.SliceStable(snapshot.Locations, func(i, j int) bool {
		return lessNamed(snapshot.Locations[i].Name, snapshot.Locations[i].ID, snapshot.Locations[j].Name, snapshot.Locations[j].ID)
	})

	sort.SliceStable(snapshot.Groups, func(i, j int) bool {
		leftLocation := locationNames[snapshot.Groups[i].LocationID]
		rightLocation := locationNames[snapshot.Groups[j].LocationID]
		if !equalFold(leftLocation, rightLocation) {
			return lessNamed(leftLocation, snapshot.Groups[i].LocationID, rightLocation, snapshot.Groups[j].LocationID)
		}
		return lessNamed(snapshot.Groups[i].Name, snapshot.Groups[i].ID, snapshot.Groups[j].Name, snapshot.Groups[j].ID)
	})

	sort.SliceStable(snapshot.Devices, func(i, j int) bool {
		leftLocationID := groupLocationIDs[snapshot.Devices[i].GroupID]
		rightLocationID := groupLocationIDs[snapshot.Devices[j].GroupID]
		leftLocation := locationNames[leftLocationID]
		rightLocation := locationNames[rightLocationID]
		if !equalFold(leftLocation, rightLocation) {
			return lessNamed(leftLocation, leftLocationID, rightLocation, rightLocationID)
		}

		leftGroup := groupNames[snapshot.Devices[i].GroupID]
		rightGroup := groupNames[snapshot.Devices[j].GroupID]
		if !equalFold(leftGroup, rightGroup) {
			return lessNamed(leftGroup, snapshot.Devices[i].GroupID, rightGroup, snapshot.Devices[j].GroupID)
		}
		return lessNamed(snapshot.Devices[i].Name, snapshot.Devices[i].Serial, snapshot.Devices[j].Name, snapshot.Devices[j].Serial)
	})

	return snapshot
}

func lessNamed(leftName, leftID, rightName, rightID string) bool {
	left := strings.ToLower(strings.TrimSpace(leftName))
	right := strings.ToLower(strings.TrimSpace(rightName))
	if left != right {
		return left < right
	}
	return strings.ToLower(leftID) < strings.ToLower(rightID)
}

func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
