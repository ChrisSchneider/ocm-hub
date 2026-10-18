package compliance

import "encoding/json"

// The SARIF 2.1.0 subset the compliance commands write and read.

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
)

// SARIF levels.
const (
	LevelError   = "error"
	LevelWarning = "warning"
	LevelNote    = "note"
)

type SARIF struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []SARIFRun `json:"runs"`
}

type SARIFRun struct {
	Tool       SARIFTool         `json:"tool"`
	Results    []SARIFResult     `json:"results"`
	Properties map[string]string `json:"properties,omitempty"`
}

type SARIFTool struct {
	Driver SARIFDriver `json:"driver"`
}

type SARIFDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []SARIFRule `json:"rules"`
}

type SARIFText struct {
	Text string `json:"text"`
}

type SARIFRule struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	ShortDescription     SARIFText       `json:"shortDescription"`
	FullDescription      SARIFText       `json:"fullDescription"`
	Help                 SARIFText       `json:"help"`
	DefaultConfiguration SARIFRuleConfig `json:"defaultConfiguration"`
	Properties           *SARIFRuleProps `json:"properties,omitempty"`
}

type SARIFRuleConfig struct {
	Level string `json:"level"`
}

type SARIFRuleProps struct {
	Tags []string `json:"tags,omitempty"`
}

type SARIFResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    SARIFText         `json:"message"`
	Locations  []SARIFLocation   `json:"locations,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type SARIFLocation struct {
	LogicalLocations []SARIFLogicalLocation `json:"logicalLocations"`
}

type SARIFLogicalLocation struct {
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName"`
	Kind               string `json:"kind"`
}

// NewSARIF is a one-run log for driver.
func NewSARIF(driver SARIFDriver, results []SARIFResult, props map[string]string) SARIF {
	if results == nil {
		results = []SARIFResult{}
	}
	return SARIF{Schema: sarifSchema, Version: sarifVersion, Runs: []SARIFRun{{Tool: SARIFTool{Driver: driver}, Results: results, Properties: props}}}
}

// ParseSARIF reads a log written by NewSARIF (or any SARIF 2.1.0 log).
func ParseSARIF(b []byte) (SARIF, error) {
	var s SARIF
	err := json.Unmarshal(b, &s)
	return s, err
}
