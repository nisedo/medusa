// Package mcpserver exposes a running fuzzing campaign to AI agents and other tools over the Model Context Protocol
// (MCP). A client can see how the campaign is going, which code it has not reached and which tests failed, try a
// call sequence to see what it does, and add a call sequence to the corpus for the fuzzer to build on.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/crytic/medusa/fuzzing"
	"github.com/crytic/medusa/fuzzing/coverage"
	"github.com/crytic/medusa/fuzzing/report"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Path is the URL path the MCP endpoint is served at.
const Path = "/mcp"

// instructions tells a connected client how the tools fit together.
const instructions = `This server controls a running medusa fuzzing campaign for Solidity smart contracts.

A typical loop: call "status" to see whether coverage has stalled, "coverage" to find functions and lines which
were never reached, "target" for the contracts and methods you can call, then "execute_sequence" to try a call
sequence aimed at the unreached code and read its trace. Once a sequence reaches new code, pass it to
"add_sequence" so the fuzzer mutates it and runs every test against it. Use "tests" to read failed tests with the
call sequence and trace that failed them.`

// Server serves the MCP tools of a Fuzzer.
type Server struct {
	// fuzzer is the Fuzzer whose campaign the tools read and steer.
	fuzzer *fuzzing.Fuzzer

	// mcpServer is the underlying MCP server the tools are registered on.
	mcpServer *mcp.Server
}

// New creates a Server for the provided Fuzzer. The version is reported to clients as the server version.
func New(fuzzer *fuzzing.Fuzzer, version string) *Server {
	server := &Server{
		fuzzer:    fuzzer,
		mcpServer: mcp.NewServer(&mcp.Implementation{Name: "medusa", Version: version}, &mcp.ServerOptions{Instructions: instructions}),
	}

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name:        "status",
		Annotations: readOnly,
		Description: "How the campaign is going: its outcome so far, how long it has run, metrics (calls and sequences " +
			"tested, corpus size, branches hit, when coverage last increased) and how many tests are in each status.",
	}, server.status)
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name:        "target",
		Annotations: readOnly,
		Description: "The deployed contracts a call sequence can call, with their addresses and method signatures, and " +
			"the addresses calls can be sent from.",
	}, server.target)
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name:        "tests",
		Annotations: readOnly,
		Description: "The tests of the campaign. A failed test has the call sequence which failed it and a structured " +
			"execution trace showing which call failed and why.",
	}, server.tests)
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name:        "coverage",
		Annotations: readOnly,
		Description: "Which code the campaign has reached. Without a file: per source file, the line ranges never " +
			"executed, the line ranges only executed by reverting calls, and whether each function was reached. With " +
			"a file: also its source, line by line, marked + (executed), r (only executed by reverting calls), " +
			"- (never executed) or blank (not executable).",
	}, server.coverage)
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name:        "execute_sequence",
		Annotations: readOnly,
		Description: "Run a sequence of calls from the state every fuzzed sequence starts from, and report what each " +
			"call did: its execution trace, and whether it reached code the corpus has not. Nothing is changed: no " +
			"coverage is recorded, nothing is added to the corpus and no test is run. Use it to check a sequence " +
			"does what you intend before adding it.",
	}, server.executeSequence)
	mcp.AddTool(server.mcpServer, &mcp.Tool{
		Name: "add_sequence",
		Description: "Run a sequence of calls like execute_sequence, then add it to the corpus. A fuzzer worker " +
			"replays it shortly after: every test is run against it, the coverage it reaches is recorded, and the " +
			"fuzzer starts mutating it. Use it for a sequence which reaches code the fuzzer has not reached on its own.",
	}, server.addSequence)
	return server
}

// MCPServer returns the underlying MCP server, to connect it to a transport other than HTTP.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}

// Handler returns the HTTP handler which serves the MCP endpoint at Path. Requests from other origins are rejected,
// as are requests which reach a localhost address under another host name, so a web page cannot reach the server.
func (s *Server) Handler() http.Handler {
	streamableHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcpServer }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	mux := http.NewServeMux()
	mux.Handle(Path, http.NewCrossOriginProtection().Handler(streamableHandler))
	return mux
}

// Listen starts serving the MCP endpoint on the provided port of the loopback interface, so it is only reachable
// from this machine. A port of 0 picks a free port.
// Returns the address the server listens on, a function which stops the server, or an error if it cannot listen.
func (s *Server) Listen(port int) (net.Addr, func(), error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, nil, err
	}
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		// Serve returns http.ErrServerClosed once the server is stopped, which is expected.
		_ = httpServer.Serve(listener)
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			_ = httpServer.Close()
		}
	}
	return listener.Addr(), stop, nil
}

// noInput is the input of a tool which takes no arguments.
type noInput struct{}

// statusOutput is the output of the status tool.
type statusOutput struct {
	// Outcome describes how the campaign is going or how it ended.
	Outcome report.Outcome `json:"outcome"`
	// Error is the error the campaign ended with, if any.
	Error string `json:"error,omitempty"`
	// DurationSeconds is how long the campaign has been running.
	DurationSeconds float64 `json:"durationSeconds"`
	// Metrics describes how much work the campaign has done.
	Metrics report.Metrics `json:"metrics"`
	// Tests is the number of tests in each status.
	Tests map[report.TestStatus]int `json:"tests"`
}

// status handles the status tool.
func (s *Server) status(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, statusOutput, error) {
	campaignReport := s.fuzzer.ReportWithoutCoverage()
	output := statusOutput{
		Outcome:         campaignReport.Outcome,
		Error:           campaignReport.Error,
		DurationSeconds: campaignReport.DurationSeconds,
		Metrics:         campaignReport.Metrics,
		Tests:           make(map[report.TestStatus]int),
	}
	for _, test := range campaignReport.Tests {
		output.Tests[test.Status]++
	}
	return nil, output, nil
}

// targetOutput is the output of the target tool.
type targetOutput struct {
	// Contracts are the deployed contracts a call sequence can call.
	Contracts []fuzzing.DeployedContract `json:"contracts"`
	// Senders are the addresses the fuzzer sends calls from.
	Senders []string `json:"senders"`
}

// target handles the target tool.
func (s *Server) target(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, targetOutput, error) {
	contracts, err := s.fuzzer.DeployedContracts()
	if err != nil {
		return nil, targetOutput{}, err
	}
	output := targetOutput{Contracts: contracts, Senders: make([]string, 0)}
	for _, sender := range s.fuzzer.SenderAddresses() {
		output.Senders = append(output.Senders, sender.Hex())
	}
	return nil, output, nil
}

// testsInput is the input of the tests tool.
type testsInput struct {
	// Status limits the tests to those in the provided status.
	Status string `json:"status,omitempty" jsonschema:"Only return tests in this status: notStarted, running, passed or failed. Defaults to all tests"`
}

// testsOutput is the output of the tests tool.
type testsOutput struct {
	// Tests are the tests of the campaign.
	Tests []report.Test `json:"tests"`
}

// tests handles the tests tool. Its output, like that of the sequence tools, is declared as any: it contains call
// sequences and traces, which define their own JSON encoding, so no output schema can be derived for them.
func (s *Server) tests(_ context.Context, _ *mcp.CallToolRequest, input testsInput) (*mcp.CallToolResult, any, error) {
	switch report.TestStatus(input.Status) {
	case "", report.TestStatusNotStarted, report.TestStatusRunning, report.TestStatusPassed, report.TestStatusFailed:
	default:
		return nil, nil, fmt.Errorf("unknown status %q: expected notStarted, running, passed or failed", input.Status)
	}
	output := testsOutput{Tests: make([]report.Test, 0)}
	for _, test := range s.fuzzer.ReportWithoutCoverage().Tests {
		if input.Status == "" || test.Status == report.TestStatus(input.Status) {
			output.Tests = append(output.Tests, test)
		}
	}
	return nil, output, nil
}

// coverageInput is the input of the coverage tool.
type coverageInput struct {
	// File limits the output to one source file and adds its source, line by line.
	File string `json:"file,omitempty" jsonschema:"Path of one source file, or the end of its path, to also get its source line by line. Defaults to a summary of every file"`
}

// coverageOutput is the output of the coverage tool.
type coverageOutput struct {
	// LinesCovered is the number of executable lines which were executed, across the files in the output.
	LinesCovered int `json:"linesCovered"`
	// LinesTotal is the number of executable lines, across the files in the output.
	LinesTotal int `json:"linesTotal"`
	// Files describes the coverage of each source file.
	Files []coverage.FileSummary `json:"files"`
	// Source is the source of the requested file, line by line, each prefixed by its coverage marker and number.
	Source string `json:"source,omitempty"`
}

// coverage handles the coverage tool.
func (s *Server) coverage(_ context.Context, _ *mcp.CallToolRequest, input coverageInput) (*mcp.CallToolResult, coverageOutput, error) {
	sourceAnalysis, err := s.fuzzer.SourceCoverage()
	if err != nil {
		return nil, coverageOutput{}, err
	}
	if sourceAnalysis == nil {
		return nil, coverageOutput{}, errors.New("no coverage is available: coverage is disabled, or the campaign has not started fuzzing yet")
	}
	summary := sourceAnalysis.Summary()
	if input.File == "" {
		return nil, coverageOutput{LinesCovered: summary.LinesCovered, LinesTotal: summary.LinesTotal, Files: summary.Files}, nil
	}

	// Find the one file whose path is, or ends with, the requested path.
	matches := make([]coverage.FileSummary, 0, 1)
	for _, file := range summary.Files {
		if file.Path == input.File {
			matches = []coverage.FileSummary{file}
			break
		}
		if strings.HasSuffix(file.Path, input.File) {
			matches = append(matches, file)
		}
	}
	if len(matches) != 1 {
		paths := make([]string, len(summary.Files))
		for i, file := range summary.Files {
			paths[i] = file.Path
		}
		return nil, coverageOutput{}, fmt.Errorf("%d source files match %q; source files: %s", len(matches), input.File, strings.Join(paths, ", "))
	}
	file := matches[0]
	return nil, coverageOutput{
		LinesCovered: file.LinesCovered,
		LinesTotal:   file.LinesTotal,
		Files:        matches,
		Source:       sourceAnalysis.Files[file.Path].AnnotatedSource(),
	}, nil
}

// sequenceInput is the input of the execute_sequence and add_sequence tools.
type sequenceInput struct {
	// Calls are the calls of the sequence, in order.
	Calls []fuzzing.CallInput `json:"calls" jsonschema:"The calls of the sequence, in the order to make them"`
}

// executeSequence handles the execute_sequence tool.
func (s *Server) executeSequence(_ context.Context, _ *mcp.CallToolRequest, input sequenceInput) (*mcp.CallToolResult, any, error) {
	result, err := s.fuzzer.ExecuteSequence(input.Calls)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// addSequence handles the add_sequence tool.
func (s *Server) addSequence(_ context.Context, _ *mcp.CallToolRequest, input sequenceInput) (*mcp.CallToolResult, any, error) {
	result, err := s.fuzzer.AddSequence(input.Calls)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}
