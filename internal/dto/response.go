package dto

import BaseApp "RecoderAgent/internal/BaseApp"

type ApplyConfigResponse struct {
	Message string         `json:"message"`
	Config  BaseApp.Config `json:"config"`
}
