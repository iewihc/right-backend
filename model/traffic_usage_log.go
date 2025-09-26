package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TrafficUsageLog represents a log entry for traffic API usage.
type TrafficUsageLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Service   string             `bson:"service"`
	API       string             `bson:"api"`
	Params    string             `bson:"params"`
	CreatedBy string             `bson:"created_by,omitempty"`
	Fleet     string             `bson:"fleet,omitempty"`
	Elements  int                `bson:"elements,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
}
