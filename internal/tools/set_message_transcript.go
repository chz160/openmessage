package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/maxghenis/openmessage/internal/app"
)

func setMessageTranscriptTool() mcp.Tool {
	return mcp.NewTool("set_message_transcript",
		mcp.WithDescription(
			"Save a transcript for an existing audio or voice message. The audio metadata is preserved and calling again overwrites the prior transcript.",
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The message_id of the audio message to transcribe."),
		),
		mcp.WithString("transcript",
			mcp.Required(),
			mcp.Description("The transcribed text. Empty string clears any existing transcript."),
		),
		mcp.WithString("model",
			mcp.Description("Free-form model identifier (for example faster-whisper:base.en)."),
		),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func setMessageTranscriptHandler(a *app.App) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		messageID := strArg(args, "message_id")
		transcript := strArg(args, "transcript")
		model := strArg(args, "model")
		if messageID == "" {
			return errorResult("set_message_transcript: message_id is required"), nil
		}
		if err := a.Store.SetMessageTranscript(messageID, transcript, model); err != nil {
			return errorResult(fmt.Sprintf("set_message_transcript: %v", err)), nil
		}
		msg, _ := a.Store.GetMessageByID(messageID)
		if msg != nil && a.OnMessagesChange != nil {
			a.OnMessagesChange(msg.ConversationID)
		}
		return textResult(fmt.Sprintf(
			"Transcript saved for message %s (%d chars, model=%q).",
			messageID, len(transcript), model,
		)), nil
	}
}
