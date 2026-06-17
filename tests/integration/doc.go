// Package integration contains integration tests that run against real databases.
// These tests require the "integration" build tag and are not run by default.
//
// The blank imports below ensure go mod tidy retains the Firestore and MongoDB
// dependencies that are used by _test.go files behind the integration build tag.
package integration

import (
	_ "cloud.google.com/go/firestore"
	_ "go.mongodb.org/mongo-driver/v2/bson"
	_ "go.mongodb.org/mongo-driver/v2/mongo"
	_ "go.mongodb.org/mongo-driver/v2/mongo/options"
)
