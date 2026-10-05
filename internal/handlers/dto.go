package handlers

// UserRequest is used to create or update a user without exposing passwords.
type UserRequest struct {
	Name  string `json:"name" example:"Aqib Ahmed"`
	Email string `json:"email" example:"aqib@example.com"`
}

// HealthResponse describes service and dependency health.
type HealthResponse struct {
	Status   string `json:"status" example:"UP"`
	Database string `json:"database,omitempty" example:"connected"`
}
