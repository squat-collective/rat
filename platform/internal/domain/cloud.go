// Package domain — cloud credential vending types.
//
// CloudCredentials is the platform-side representation of credentials returned
// by a Pro cloud provider plugin (cloud-aws, cloud-gcp, …). It is the type
// exchanged across the api.CloudProvider interface boundary, deliberately
// decoupled from the wire proto (cloudv1.GetCredentialsResponse) so platform
// callers and tests do not have to import the generated package.
package domain

import "time"

// CloudCredentials carries scoped, short-lived credentials vended by a cloud
// provider plugin (e.g., AWS STS AssumeRole, GCP federated tokens, Azure SAS).
//
// All fields except Expiry are strings — they are passed through to the
// executor as environment overrides on the run. SessionToken is empty for
// long-lived IAM users; populated for STS/temporary credentials. Region is the
// cloud region (e.g., "us-east-1") and may be empty when the provider does not
// need a regional hint.
//
// The platform never persists these. They flow ratd → executor → runner on
// each pipeline submission and live only as long as the run.
type CloudCredentials struct {
	AccessKey    string    `json:"access_key"`
	SecretKey    string    `json:"secret_key"`
	SessionToken string    `json:"session_token,omitempty"`
	Region       string    `json:"region,omitempty"`
	Expiry       time.Time `json:"expiry"`
}
