// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	pinnedAction     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+@[0-9a-f]{40}$`)
	secretUsePattern = regexp.MustCompile(`secrets\s*\.([A-Za-z0-9_]+)|secrets\s*\[\s*['"]([A-Za-z0-9_]+)['"]\s*\]`)
	// qualifiedGITHUBToken matches the only allowed secret read, so the
	// bare-token scan below does not flag it. The trailing boundary keeps
	// secrets.GITHUB_TOKEN_SUFFIX from stripping down to a bare suffix.
	qualifiedGITHUBToken = regexp.MustCompile(`secrets\s*\.\s*GITHUB_TOKEN\b|secrets\s*\[\s*['"]GITHUB_TOKEN['"]\s*\]`)
	bareSecretsToken     = regexp.MustCompile(`\bsecrets\b`)
)

const (
	// tagShapeCheck is the exact tag filter the workflow must run before it
	// publishes anything.
	tagShapeCheck = `grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'`
	// signerIdentity is the certificate identity a keyless signature from this
	// workflow carries, derived from the running workflow reference.
	signerIdentity = "https://github.com/${{ github.workflow_ref }}"
	// oidcIssuerFlag pins the GitHub Actions OIDC issuer on the verify step.
	oidcIssuerFlag = `--certificate-oidc-issuer "https://token.actions.githubusercontent.com"`
	// sbomFile is the SBOM the generate and attest steps must agree on.
	sbomFile = "eshu-collector-gcp-cloud.spdx.json"
)

type workflowStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
	// If holds the step's `if:` condition; ContinueOnError holds
	// `continue-on-error:`. Both default to the zero value when absent.
	// contractProblems requires the guard steps to leave both zero: a
	// skipped or tolerated guard would publish past a failing check.
	If              string `yaml:"if"`
	ContinueOnError bool   `yaml:"continue-on-error"`
}

// needsList accepts the scalar (`needs: changes`) and sequence
// (`needs: [a, b]`) forms GitHub allows.
type needsList []string

func (n *needsList) UnmarshalYAML(value *yaml.Node) error {
	var single string
	if err := value.Decode(&single); err == nil {
		*n = []string{single}
		return nil
	}
	var multi []string
	if err := value.Decode(&multi); err != nil {
		return err
	}
	*n = multi
	return nil
}

type workflowFile struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency map[string]any    `yaml:"concurrency"`
	Jobs        map[string]struct {
		Needs       needsList         `yaml:"needs"`
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Timeout     int               `yaml:"timeout-minutes"`
		Env         map[string]string `yaml:"env"`
		Steps       []workflowStep    `yaml:"steps"`
	} `yaml:"jobs"`
}

// stepIndex returns the position of the first step for which match is true,
// or -1.
func stepIndex(steps []workflowStep, match func(workflowStep) bool) int {
	for i, step := range steps {
		if match(step) {
			return i
		}
	}
	return -1
}

// tagOnlyGate is the exact step condition scoping the tag guards to tag
// events, so a main push is never gated on a tag. branchOnlyGate scopes the
// branch metadata step to branch events.
const (
	tagOnlyGate    = "github.ref_type == 'tag'"
	branchOnlyGate = "github.ref_type == 'branch'"
)

// contractProblems checks the steps that make a published image trustworthy:
// the tag and manifest checks run before anything is pushed on tag events,
// one platform is built, the signature is verified against a pinned identity
// and issuer, and the provenance and SBOM attestations are generated and
// verified.
func contractProblems(steps []workflowStep) []string {
	var problems []string
	tagIdx := stepIndex(steps, func(s workflowStep) bool { return strings.Contains(s.Run, tagShapeCheck) })
	if tagIdx < 0 {
		problems = append(problems, "no step filters the tag with "+tagShapeCheck)
	}
	versionIdx := stepIndex(steps, func(s workflowStep) bool {
		return strings.Contains(s.Run, "manifest.yaml") && strings.Contains(s.Run, "TAG_NAME") && strings.Contains(s.Run, "exit 1")
	})
	if versionIdx < 0 {
		problems = append(problems, "no step fails unless the tag matches manifest.yaml metadata.version")
	}
	publishIdx := stepIndex(steps, func(s workflowStep) bool { return fmt.Sprint(s.With["push"]) == "true" })
	if publishIdx < 0 {
		problems = append(problems, "no step pushes the image")
	} else {
		if tagIdx >= 0 && tagIdx > publishIdx {
			problems = append(problems, "the tag shape check runs after the image is pushed")
		}
		if versionIdx >= 0 && versionIdx > publishIdx {
			problems = append(problems, "the manifest version check runs after the image is pushed")
		}
		if got := fmt.Sprint(steps[publishIdx].With["platforms"]); got != "linux/amd64" {
			problems = append(problems, fmt.Sprintf("build platforms = %q, want exactly linux/amd64", got))
		}
	}
	mainHeadIdx := stepIndex(steps, func(s workflowStep) bool {
		return strings.Contains(s.Run, "git rev-parse origin/main") && strings.Contains(s.Run, "exit 1")
	})
	if mainHeadIdx < 0 {
		problems = append(problems, "no step requires the tag to point at the protected main head")
	} else if publishIdx >= 0 && mainHeadIdx > publishIdx {
		problems = append(problems, "the main-head check runs after the image is pushed")
	}
	verifyIdx := stepIndex(steps, func(s workflowStep) bool { return strings.Contains(s.Run, "cosign verify") })
	if verifyIdx < 0 {
		problems = append(problems, "no step runs cosign verify")
	} else {
		verify := steps[verifyIdx]
		if verify.Env["CERTIFICATE_IDENTITY"] != signerIdentity {
			problems = append(problems, fmt.Sprintf("cosign identity = %q, want %q", verify.Env["CERTIFICATE_IDENTITY"], signerIdentity))
		}
		for _, want := range []string{`--certificate-identity "${CERTIFICATE_IDENTITY}"`, oidcIssuerFlag} {
			if !strings.Contains(verify.Run, want) {
				problems = append(problems, "cosign verify lacks "+want)
			}
		}
		if strings.Contains(verify.Run, "regexp") {
			problems = append(problems, "cosign verify matches the identity by regular expression, want the exact identity")
		}
	}
	attestIdx := stepIndex(steps, func(s workflowStep) bool { return strings.Contains(s.Run, "gh attestation verify") })
	if attestIdx < 0 {
		problems = append(problems, "no step runs gh attestation verify")
	} else {
		run := steps[attestIdx].Run
		if strings.Count(run, "gh attestation verify") < 2 {
			problems = append(problems, "gh attestation verify must run for provenance and for the SBOM")
		}
		for _, want := range []string{`-R "${GITHUB_REPOSITORY}"`, "--predicate-type https://spdx.dev/Document/v2.3"} {
			if !strings.Contains(run, want) {
				problems = append(problems, "gh attestation verify lacks "+want)
			}
		}
	}
	for uses, input := range map[string]string{"anchore/sbom-action@": "output-file", "actions/attest@": "sbom-path"} {
		idx := stepIndex(steps, func(s workflowStep) bool { return strings.HasPrefix(s.Uses, uses) })
		if idx < 0 {
			problems = append(problems, "no step uses "+uses)
		} else if got := fmt.Sprint(steps[idx].With[input]); got != sbomFile {
			problems = append(problems, fmt.Sprintf("%s input %s = %q, want %q", uses, input, got, sbomFile))
		}
	}
	for _, guard := range []struct {
		name string
		idx  int
		want string
	}{
		{"tag shape check", tagIdx, tagOnlyGate},
		{"manifest version check", versionIdx, tagOnlyGate},
		{"main-head check", mainHeadIdx, tagOnlyGate},
		{"cosign verify", verifyIdx, ""},
		{"gh attestation verify", attestIdx, ""},
	} {
		if guard.idx < 0 {
			continue
		}
		step := steps[guard.idx]
		if step.If != guard.want {
			if guard.want == "" {
				problems = append(problems, fmt.Sprintf("the %s runs under %q, want it unconditional", guard.name, step.If))
			} else {
				problems = append(problems, fmt.Sprintf("the %s runs under %q, want %q so a main push is not gated on a tag", guard.name, step.If, guard.want))
			}
		}
		if step.ContinueOnError {
			problems = append(problems, fmt.Sprintf("the %s sets continue-on-error, want its failure to fail the job", guard.name))
		}
	}
	return problems
}

// metaProblems checks the dual metadata steps that tag tag events and branch
// events independently: the tag step must emit exactly the tag ref, the
// branch step must emit main, latest, and the long sha tag, and the build
// step must publish the concatenation of both so a tag event pushes exactly
// what the tag-only workflow pushed.
func metaProblems(steps []workflowStep) []string {
	var problems []string
	count := 0
	for _, step := range steps {
		if !strings.HasPrefix(step.Uses, "docker/metadata-action@") {
			continue
		}
		count++
		switch step.If {
		case tagOnlyGate, branchOnlyGate:
		default:
			problems = append(problems, fmt.Sprintf("metadata step %q runs under %q, want %q or %q", step.Name, step.If, tagOnlyGate, branchOnlyGate))
		}
		if step.ContinueOnError {
			problems = append(problems, fmt.Sprintf("metadata step %q sets continue-on-error, want a failed tag set to fail the job", step.Name))
		}
	}
	if count != 2 {
		return append(problems, fmt.Sprintf("found %d docker/metadata-action steps, want exactly 2 (one per ref type)", count))
	}
	tagIdx := stepIndex(steps, func(s workflowStep) bool {
		return strings.HasPrefix(s.Uses, "docker/metadata-action@") && s.If == tagOnlyGate
	})
	branchIdx := stepIndex(steps, func(s workflowStep) bool {
		return strings.HasPrefix(s.Uses, "docker/metadata-action@") && s.If == branchOnlyGate
	})
	if tagIdx < 0 {
		problems = append(problems, "no metadata step runs for tag events")
	} else if got := fmt.Sprint(steps[tagIdx].With["tags"]); !strings.Contains(got, "type=ref,event=tag") {
		problems = append(problems, fmt.Sprintf("tag metadata tags = %q, want type=ref,event=tag", got))
	}
	if branchIdx < 0 {
		problems = append(problems, "no metadata step runs for branch events")
	} else {
		got := fmt.Sprint(steps[branchIdx].With["tags"])
		for _, want := range []string{"type=raw,value=main", "type=raw,value=latest", "type=sha,prefix=sha-,format=long"} {
			if !strings.Contains(got, want) {
				problems = append(problems, fmt.Sprintf("branch metadata tags = %q, want %s", got, want))
			}
		}
	}
	publishIdx := stepIndex(steps, func(s workflowStep) bool { return fmt.Sprint(s.With["push"]) == "true" })
	if publishIdx >= 0 {
		tags := fmt.Sprint(steps[publishIdx].With["tags"])
		for _, want := range []string{"steps.meta-tag.outputs.tags", "steps.meta-branch.outputs.tags"} {
			if !strings.Contains(tags, want) {
				problems = append(problems, fmt.Sprintf("build tags = %q, want both %s", tags, want))
			}
		}
		labels := fmt.Sprint(steps[publishIdx].With["labels"])
		for _, want := range []string{"steps.meta-tag.outputs.labels", "steps.meta-branch.outputs.labels"} {
			if !strings.Contains(labels, want) {
				problems = append(problems, fmt.Sprintf("build labels = %q, want both %s", labels, want))
			}
		}
	}
	return problems
}

// releaseWorkflowProblems returns every way raw breaks the release workflow's
// safety properties: it must run only for pushes to main and pushed v* tags,
// serialize image publishes, hold only the permissions publishing and
// attesting an image need, use no secret but GITHUB_TOKEN, pin every action
// by full commit SHA, deploy nothing, and keep the tag, platform, signature,
// and attestation checks.
func releaseWorkflowProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{fmt.Sprintf("release.yml does not parse: %v", err)}
	}
	var problems []string
	pushValue, pushPresent := workflow.On["push"]
	push, pushTyped := pushValue.(map[string]any)
	tagsValue, tagsPresent := push["tags"]
	tags, tagsTyped := tagsValue.([]any)
	branchesValue, branchesPresent := push["branches"]
	branches, branchesTyped := branchesValue.([]any)
	if len(workflow.On) != 1 || !pushPresent || !pushTyped || len(push) != 2 ||
		!tagsPresent || !tagsTyped || len(tags) != 1 || tags[0] != "v*" ||
		!branchesPresent || !branchesTyped || len(branches) != 1 || branches[0] != "main" {
		problems = append(problems, fmt.Sprintf("triggers = %v, want push of branches [main] and tags [v*]", workflow.On))
	}
	group, groupOk := workflow.Concurrency["group"].(string)
	cancel, cancelOk := workflow.Concurrency["cancel-in-progress"].(bool)
	if len(workflow.Concurrency) != 2 || !groupOk || group != "image-publish-${{ github.ref }}" || !cancelOk || cancel {
		problems = append(problems, fmt.Sprintf("concurrency = %v, want group image-publish-${{ github.ref }} with cancel-in-progress: false", workflow.Concurrency))
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		problems = append(problems, fmt.Sprintf("workflow permissions = %v, want contents: read only", workflow.Permissions))
	}
	want := map[string]string{"contents": "read", "packages": "write", "id-token": "write", "attestations": "write"}
	for name, job := range workflow.Jobs {
		if len(job.Permissions) != len(want) {
			problems = append(problems, fmt.Sprintf("job %s permissions = %v, want %v", name, job.Permissions, want))
		}
		for key, value := range want {
			if job.Permissions[key] != value {
				problems = append(problems, fmt.Sprintf("job %s permission %s = %q, want %q", name, key, job.Permissions[key], value))
			}
		}
		for _, step := range job.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				problems = append(problems, fmt.Sprintf("job %s step %q uses %q, want a full commit SHA pin", name, step.Name, step.Uses))
			}
			for _, word := range []string{"kubectl", "helm ", "argocd", "terraform", "aws "} {
				if strings.Contains(step.Run, word) {
					problems = append(problems, fmt.Sprintf("job %s step %q runs %q: the release workflow must not deploy", name, step.Name, word))
				}
			}
		}
	}
	image, ok := workflow.Jobs["image"]
	if !ok || len(workflow.Jobs) != 1 {
		problems = append(problems, "want exactly one job, named image")
	}
	problems = append(problems, contractProblems(image.Steps)...)
	problems = append(problems, metaProblems(image.Steps)...)
	for _, match := range secretUsePattern.FindAllStringSubmatch(string(raw), -1) {
		name := match[1]
		if name == "" {
			name = match[2]
		}
		if name != "GITHUB_TOKEN" {
			problems = append(problems, "uses secret "+name+", want only GITHUB_TOKEN")
		}
	}
	stripped := qualifiedGITHUBToken.ReplaceAllString(string(raw), "")
	if bareSecretsToken.MatchString(stripped) {
		problems = append(problems, "reads secrets outside a qualified GITHUB_TOKEN access, want only GITHUB_TOKEN")
	}
	if strings.Contains(string(raw), "pull_request") {
		problems = append(problems, "mentions pull_request: nothing may publish on a PR")
	}
	return problems
}

func TestReleaseWorkflowShape(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := releaseWorkflowProblems(raw); len(problems) > 0 {
		t.Fatalf("release.yml:\n%s", strings.Join(problems, "\n"))
	}
}

// TestReleaseWorkflowGateRejectsViolations feeds the check synthetic
// workflows so it is proven to fail, not only to pass on the checked-in file.
func TestReleaseWorkflowGateRejectsViolations(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		"pull request trigger": func(s string) string { return strings.Replace(s, "on:\n  push:", "on:\n  pull_request:\n  push:", 1) },
		"branch trigger widened": func(s string) string {
			return strings.Replace(s, "      - main\n", "      - main\n      - develop\n", 1)
		},
		"tag pattern widened": func(s string) string { return strings.Replace(s, `- "v*"`, `- "*"`, 1) },
		"tag-pinned action": func(s string) string {
			return strings.Replace(s, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "actions/checkout@v5", 1)
		},
		"extra secret":   func(s string) string { return s + "\n# ${{ secrets.DEPLOY_KEY }}\n" },
		"bracket secret": func(s string) string { return s + "\n# ${{ secrets['DEPLOY_KEY'] }}\n" },
		"whitespace bracket secret": func(s string) string {
			return s + "\n# ${{ secrets ['DEPLOY_KEY'] }}\n"
		},
		"toJSON secret store": func(s string) string { return s + "\n# ${{ toJSON(secrets) }}\n" },
		"GITHUB_TOKEN suffix impersonation": func(s string) string {
			return s + "\n# ${{ secrets.GITHUB_TOKEN_SUFFIX }}\n"
		},
		"continue-on-error on version check": func(s string) string {
			return strings.Replace(s, "- name: Require the tag to match the manifest version\n", "- name: Require the tag to match the manifest version\n        continue-on-error: true\n", 1)
		},
		"if-never on version check": func(s string) string {
			return strings.Replace(s, "- name: Require the tag to match the manifest version\n", "- name: Require the tag to match the manifest version\n        if: github.event_name == 'never'\n", 1)
		},
		"tag guard ungated": func(s string) string {
			return strings.Replace(s, "        if: github.ref_type == 'tag'\n", "", 1)
		},
		"verify step gated on tag": func(s string) string {
			return strings.Replace(s, "- name: Verify the image signature\n", "- name: Verify the image signature\n        if: github.ref_type == 'tag'\n", 1)
		},
		"branch metadata gated on tag": func(s string) string {
			return strings.Replace(s, "id: meta-branch\n        if: github.ref_type == 'branch'", "id: meta-branch\n        if: github.ref_type == 'tag'", 1)
		},
		"concurrency cancelled": func(s string) string {
			return strings.Replace(s, "cancel-in-progress: false", "cancel-in-progress: true", 1)
		},
		"latest tag dropped": func(s string) string {
			return strings.Replace(s, "            type=raw,value=latest\n", "", 1)
		},
		"main-head check weakened": func(s string) string {
			return strings.Replace(s, "git rev-parse origin/main", "git rev-parse HEAD", 1)
		},
		"extra permission": func(s string) string {
			return strings.Replace(s, "      attestations: write\n", "      attestations: write\n      actions: write\n", 1)
		},
		"deploy step": func(s string) string {
			return strings.Replace(s, "cosign sign --yes", "kubectl apply -f x && cosign sign --yes", 1)
		},
		"tag regex open": func(s string) string {
			return strings.Replace(s, `'^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'`, `'^v.*$'`, 1)
		},
		"platform adds arm64": func(s string) string {
			return strings.Replace(s, "platforms: linux/amd64", "platforms: linux/amd64,linux/arm64", 1)
		},
		"verify identity by regexp": func(s string) string {
			return strings.Replace(s, `--certificate-identity "${CERTIFICATE_IDENTITY}"`, `--certificate-identity-regexp '.*'`, 1)
		},
		"verify identity from a literal": func(s string) string {
			return strings.Replace(s, "CERTIFICATE_IDENTITY: https://github.com/${{ github.workflow_ref }}", "CERTIFICATE_IDENTITY: https://github.com/eshu-hq", 1)
		},
		"verify issuer dropped": func(s string) string {
			return strings.Replace(s, `--certificate-oidc-issuer "https://token.actions.githubusercontent.com"`, `--certificate-oidc-issuer "https://example.invalid"`, 1)
		},
		"sbom path dropped": func(s string) string {
			return strings.Replace(s, "sbom-path: "+sbomFile, "sbom-path: other.spdx.json", 1)
		},
		"sbom attestation verify dropped": func(s string) string {
			return strings.Replace(s, "--predicate-type https://spdx.dev/Document/v2.3", "", 1)
		},
		"provenance verify dropped": func(s string) string {
			return strings.Replace(s, `gh attestation verify "${OCI_IMAGE_REF}" \
            -R "${GITHUB_REPOSITORY}"
          gh attestation verify`, "gh attestation verify", 1)
		},
		"version check dropped": func(s string) string {
			return strings.ReplaceAll(s, "manifest.yaml", "other.yaml")
		},
		"version check runs after the push": func(s string) string {
			return strings.ReplaceAll(s, "manifest.yaml", "other.yaml") +
				"      - name: Late version check\n        run: |\n          test \"${TAG_NAME}\" = \"v$(cat manifest.yaml)\" || exit 1\n"
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(releaseWorkflowProblems([]byte(mutated))) == 0 {
			t.Errorf("%s: the gate accepted a violating workflow", name)
		}
	}
}
