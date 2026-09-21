package aicopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "maaend"
	serverVersion = "1.0.0"

	// The whole project works against the 720p reference screen.
	baseWidth  = 1280
	baseHeight = 720

	clickContact        = 0
	clickDurationMillis = 80
	clickDelayMillis    = 200

	// runSummaryNodeLimit and ocrResultLimit keep a single tool result small
	// enough that repeated calls do not exhaust the AI client's context.
	runSummaryNodeLimit = 8
	ocrResultLimit      = 40
)

// emptyInput is the argument type of tools that take no parameters.
type emptyInput struct{}

type statusOutput struct {
	State    string `json:"state" jsonschema:"standby when a new command can be accepted, busy while one is running"`
	Endpoint string `json:"endpoint" jsonschema:"the MCP endpoint this session is served on"`
	Tool     string `json:"tool,omitempty" jsonschema:"the tool currently running, if any"`
	Entry    string `json:"entry,omitempty" jsonschema:"the pipeline entry currently running, if any"`
}

type taskSummary struct {
	Entry   string `json:"entry" jsonschema:"pass this to run_pipeline or get_task_detail"`
	Summary string `json:"summary" jsonschema:"one line describing what the entry does"`
}

type listTasksOutput struct {
	Tasks []taskSummary `json:"tasks"`
}

type taskDetailInput struct {
	Entry string `json:"entry" jsonschema:"an entry name returned by list_tasks"`
}

type taskDetailOutput struct {
	Entry         string `json:"entry"`
	Summary       string `json:"summary"`
	Preconditions string `json:"preconditions,omitempty" jsonschema:"the game state the entry expects before it starts"`
	Limits        string `json:"limits,omitempty" jsonschema:"what the entry deliberately does not handle"`
	OnFailure     string `json:"on_failure,omitempty" jsonschema:"the usual cause when the entry fails, and what to try next"`
}

type runPipelineInput struct {
	Entry    string `json:"entry" jsonschema:"an entry name returned by list_tasks"`
	Override string `json:"override,omitempty" jsonschema:"optional pipeline override as a JSON object string, leave empty unless you know the node names"`
}

type runPipelineOutput struct {
	Entry      string   `json:"entry"`
	Status     string   `json:"status" jsonschema:"success, failure, running, pending or invalid"`
	NodeCount  int      `json:"node_count" jsonschema:"how many nodes the run went through"`
	LastNodes  []string `json:"last_nodes,omitempty" jsonschema:"the trailing node names, newest last"`
	FailedNode string   `json:"failed_node,omitempty" jsonschema:"the node that did not finish, only set when the run failed"`
}

type clickInput struct {
	X int `json:"x" jsonschema:"horizontal position on the 1280x720 reference screen"`
	Y int `json:"y" jsonschema:"vertical position on the 1280x720 reference screen"`
}

type clickOutput struct {
	Clicked []int `json:"clicked" jsonschema:"the point that was clicked, as [x, y]"`
}

type ocrInput struct {
	ROI []int `json:"roi,omitempty" jsonschema:"optional region as [x, y, width, height] on the 1280x720 reference screen, omit to read the whole screen"`
}

type ocrItem struct {
	Text string `json:"text"`
	Box  []int  `json:"box" jsonschema:"where the text sits, as [x, y, width, height]"`
}

type ocrOutput struct {
	Texts []ocrItem `json:"texts"`
}

// newMCPServer builds the MCP surface exposed to the AI client.
func (s *session) newMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   i18n.T("aicopilot.server.title"),
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: i18n.T("aicopilot.server.instructions"),
	})

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_status",
		Description: i18n.T("aicopilot.tool.get_status"),
		Annotations: readOnly,
	}, s.getStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tasks",
		Description: i18n.T("aicopilot.tool.list_tasks"),
		Annotations: readOnly,
	}, s.listTasks)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_task_detail",
		Description: i18n.T("aicopilot.tool.get_task_detail"),
		Annotations: readOnly,
	}, s.getTaskDetail)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_pipeline",
		Description: i18n.T("aicopilot.tool.run_pipeline"),
	}, s.runPipeline)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "click",
		Description: i18n.T("aicopilot.tool.click"),
	}, s.click)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ocr",
		Description: i18n.T("aicopilot.tool.ocr"),
		Annotations: readOnly,
	}, s.ocr)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "screencap",
		Description: i18n.T("aicopilot.tool.screencap"),
		Annotations: readOnly,
	}, s.screencap)

	registerPrompts(srv)

	return srv
}

// getStatus answers from session state alone, so it stays available while a
// long run_pipeline occupies the standby loop.
func (s *session) getStatus(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, statusOutput, error) {
	return nil, s.status(), nil
}

func (s *session) listTasks(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listTasksOutput, error) {
	entries, err := loadAllowedEntries()
	if err != nil {
		return nil, listTasksOutput{}, err
	}

	out := listTasksOutput{Tasks: make([]taskSummary, 0, len(entries))}
	for _, entry := range entries {
		out.Tasks = append(out.Tasks, taskSummary{
			Entry:   entry.Entry,
			Summary: localize(entry.Summary),
		})
	}
	return nil, out, nil
}

func (s *session) getTaskDetail(_ context.Context, _ *mcp.CallToolRequest, in taskDetailInput) (*mcp.CallToolResult, taskDetailOutput, error) {
	entry, err := findAllowedEntry(in.Entry)
	if err != nil {
		return nil, taskDetailOutput{}, err
	}

	return nil, taskDetailOutput{
		Entry:         entry.Entry,
		Summary:       localize(entry.Summary),
		Preconditions: localize(entry.Preconditions),
		Limits:        localize(entry.Limits),
		OnFailure:     localize(entry.OnFailure),
	}, nil
}

func (s *session) runPipeline(reqCtx context.Context, _ *mcp.CallToolRequest, in runPipelineInput) (*mcp.CallToolResult, runPipelineOutput, error) {
	entry, err := findAllowedEntry(in.Entry)
	if err != nil {
		return nil, runPipelineOutput{}, err
	}

	out, err := submitCommand(s, reqCtx, "run_pipeline", entry.Entry, func(ctx *maa.Context) (runPipelineOutput, error) {
		return runEntry(ctx, entry.Entry, in.Override)
	})
	return nil, out, err
}

func (s *session) click(reqCtx context.Context, _ *mcp.CallToolRequest, in clickInput) (*mcp.CallToolResult, clickOutput, error) {
	out, err := submitCommand(s, reqCtx, "click", "", func(ctx *maa.Context) (clickOutput, error) {
		if err := clickAt(ctx, in.X, in.Y); err != nil {
			return clickOutput{}, err
		}
		return clickOutput{Clicked: []int{in.X, in.Y}}, nil
	})
	return nil, out, err
}

func (s *session) ocr(reqCtx context.Context, _ *mcp.CallToolRequest, in ocrInput) (*mcp.CallToolResult, ocrOutput, error) {
	out, err := submitCommand(s, reqCtx, "ocr", "", func(ctx *maa.Context) (ocrOutput, error) {
		texts, err := recognizeText(ctx, in.ROI)
		if err != nil {
			return ocrOutput{}, err
		}
		return ocrOutput{Texts: texts}, nil
	})
	return nil, out, err
}

// screencap returns the frame as image content rather than structured output,
// which is what an AI client needs in order to actually look at it.
func (s *session) screencap(reqCtx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
	encoded, err := submitCommand(s, reqCtx, "screencap", "", encodeScreenshot)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.ImageContent{Data: encoded, MIMEType: "image/png"}},
	}, nil, nil
}

// runEntry starts a white-listed pipeline entry and reduces the run to a
// summary small enough to hand back to the AI client.
func runEntry(ctx *maa.Context, entry, override string) (runPipelineOutput, error) {
	var (
		detail *maa.TaskDetail
		err    error
	)
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		// MaaFramework silently falls back to an empty override on malformed
		// JSON, which would look like a mysteriously ineffective run.
		if !json.Valid([]byte(trimmed)) {
			return runPipelineOutput{}, errors.New("override is not valid JSON")
		}
		detail, err = ctx.RunTask(entry, trimmed)
	} else {
		detail, err = ctx.RunTask(entry)
	}
	if err != nil {
		return runPipelineOutput{}, fmt.Errorf("run %q: %w", entry, err)
	}
	if detail == nil {
		return runPipelineOutput{}, fmt.Errorf("run %q produced no task detail", entry)
	}

	out := runPipelineOutput{
		Entry:     detail.Entry,
		Status:    detail.Status.String(),
		NodeCount: len(detail.Nodes),
	}

	for _, ref := range detail.Nodes[max(0, len(detail.Nodes)-runSummaryNodeLimit):] {
		nodeDetail, err := ref.GetDetail()
		if err != nil || nodeDetail == nil {
			continue
		}
		out.LastNodes = append(out.LastNodes, nodeDetail.Name)
		if detail.Status.Failure() && !nodeDetail.RunCompleted {
			out.FailedNode = nodeDetail.Name
		}
	}
	return out, nil
}

func clickAt(ctx *maa.Context, x, y int) error {
	if x < 0 || x >= baseWidth || y < 0 || y >= baseHeight {
		return fmt.Errorf("point (%d, %d) is outside the %dx%d reference screen", x, y, baseWidth, baseHeight)
	}

	adaptor, err := control.NewControlAdaptor(ctx, ctx.GetTasker().GetController(), baseWidth, baseHeight)
	if err != nil {
		return fmt.Errorf("create the control adaptor: %w", err)
	}

	adaptor.TouchClick(clickContact, x, y, clickDurationMillis, clickDelayMillis)
	return nil
}

func recognizeText(ctx *maa.Context, roi []int) ([]ocrItem, error) {
	img, err := capture(ctx)
	if err != nil {
		return nil, err
	}

	region, err := ocrRegion(roi, img.Bounds())
	if err != nil {
		return nil, err
	}

	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		maa.OCRParam{ROI: maa.NewTargetRect(region)},
		img,
	)
	if err != nil {
		return nil, fmt.Errorf("run OCR: %w", err)
	}
	return collectTexts(detail), nil
}

// ocrRegion falls back to the whole frame when the AI client omits the region.
func ocrRegion(roi []int, bounds image.Rectangle) (maa.Rect, error) {
	if len(roi) == 0 {
		return maa.Rect{0, 0, bounds.Dx(), bounds.Dy()}, nil
	}
	if len(roi) != 4 {
		return maa.Rect{}, fmt.Errorf("roi needs exactly 4 values [x, y, width, height], got %d", len(roi))
	}
	if roi[2] <= 0 || roi[3] <= 0 {
		return maa.Rect{}, fmt.Errorf("roi width and height must be positive, got [%d, %d]", roi[2], roi[3])
	}
	return maa.Rect{roi[0], roi[1], roi[2], roi[3]}, nil
}

func collectTexts(detail *maa.RecognitionDetail) []ocrItem {
	if detail == nil || detail.Results == nil {
		return nil
	}

	items := make([]ocrItem, 0, len(detail.Results.All))
	for _, result := range detail.Results.All {
		if result == nil {
			continue
		}
		ocr, ok := result.AsOCR()
		if !ok {
			continue
		}
		text := strings.TrimSpace(ocr.Text)
		if text == "" {
			continue
		}

		items = append(items, ocrItem{
			Text: text,
			Box:  []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()},
		})
		if len(items) >= ocrResultLimit {
			break
		}
	}
	return items
}

func encodeScreenshot(ctx *maa.Context) ([]byte, error) {
	img, err := capture(ctx)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode the screenshot: %w", err)
	}
	return buf.Bytes(), nil
}

func capture(ctx *maa.Context) (image.Image, error) {
	controller := ctx.GetTasker().GetController()
	if !controller.PostScreencap().Wait().Success() {
		return nil, errors.New("the controller failed to take a screenshot")
	}

	img, err := controller.CacheImage()
	if err != nil {
		return nil, fmt.Errorf("read the captured frame: %w", err)
	}
	return img, nil
}
