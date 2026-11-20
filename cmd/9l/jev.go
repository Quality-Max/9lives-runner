package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Quality-Max/9lives-runner/internal/jev"
)

func jevPreflight(config jev.Config, format string, out, errOut io.Writer) int {
	report, err := jev.CheckConfig(config)
	if err != nil {
		fmt.Fprintln(errOut, "9l: invalid Jev model or image-upload policy")
		return 2
	}
	if format == "json" {
		if json.NewEncoder(out).Encode(report) != nil {
			return 2
		}
	} else {
		if _, err := fmt.Fprintf(out, "Jev: %s\nModel: %s\nImages: %s\nCredential present: %t\nConnectivity/model availability: not checked (no API request)\n", report.Status, report.Model, report.Images, report.CredentialPresent); err != nil {
			return 2
		}
	}
	if report.Status == "missing_credential" {
		return 2
	}
	return 0
}
