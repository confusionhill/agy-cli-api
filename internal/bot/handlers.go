package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Handle Callback Queries (inline keyboard taps)
	if update.CallbackQuery != nil {
		b.handleCallback(update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	msg := update.Message
	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Check authorization
	if !b.isUserAllowed(userID) {
		b.logger.Warn("unauthorized telegram user attempted access",
			"user_id", userID,
			"username", msg.From.UserName,
			"text", msg.Text,
		)
		reply := tgbotapi.NewMessage(chatID, "⛔ *Unauthorized*\nYour Telegram User ID is not in `TELEGRAM_ALLOWED_USER_IDS`.")
		reply.ParseMode = "Markdown"
		_, _ = b.api.Send(reply)
		return
	}

	// Handle Slash Commands
	if msg.IsCommand() {
		b.handleCommand(msg)
		return
	}

	// Handle Conversational Chat (Text and/or Photo/Document)
	b.handleChatMessage(msg)
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	cmd := strings.ToLower(msg.Command())
	args := strings.TrimSpace(msg.CommandArguments())
	sess := b.getSession(chatID)

	switch cmd {
	case "start", "help":
		b.cmdHelp(chatID, sess)
	case "status":
		b.cmdStatus(chatID, sess)
	case "new":
		b.cmdNew(chatID, sess, args)
	case "folders", "projects":
		b.cmdFolders(chatID)
	case "folder":
		b.cmdSetFolder(chatID, sess, args)
	case "convs", "chats":
		b.cmdConvs(chatID)
	case "c":
		b.cmdSwitchConv(chatID, sess, args)
	case "models":
		b.cmdModels(chatID)
	case "model":
		b.cmdSetModel(chatID, sess, args)
	case "effort":
		b.cmdSetEffort(chatID, sess, args)
	case "usage":
		b.cmdUsage(chatID)
	case "mcp":
		b.cmdMCP(chatID)
	case "cancel", "stop":
		b.cmdCancel(chatID, sess)
	default:
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Unknown command `/%s`. Type /help for available commands.", cmd))
		reply.ParseMode = "Markdown"
		_, _ = b.api.Send(reply)
	}
}

func (b *Bot) cmdHelp(chatID int64, sess *UserSession) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	activeConv := sess.ActiveConversationID
	if activeConv == "" {
		activeConv = "_None (starts new turn)_"
	}

	text := fmt.Sprintf(`🤖 *Antigravity Remote Agent*

*Active Session:*
• *Folder:* `+"`%s`"+`
• *Model:* `+"`%s`"+`
• *Effort:* `+"`%s`"+`
• *Convo:* %s

*Commands:*
/new - Start a fresh conversation
/folders - Switch target workspace folder
/convs - Pick and resume recent conversation
/models - Switch AI model
/effort - Set reasoning effort (low/med/high)
/usage - Check model token quotas
/mcp - View connected MCP servers
/cancel - Abort active generation
/status - View current session details

_Tip: You can also tap the paperclip to send photos or documents with your prompt!_`,
		sess.ActiveWorkspaceLabel,
		sess.Model,
		sess.Effort,
		activeConv,
	)

	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdStatus(chatID int64, sess *UserSession) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	activeConv := sess.ActiveConversationID
	if activeConv == "" {
		activeConv = "None (starts new turn on next message)"
	}

	text := fmt.Sprintf(`📊 *Current Session Status*

• *Workspace Folder:* `+"`%s`"+`
• *Path:* `+"`%s`"+`
• *Active Conversation:* `+"`%s`"+`
• *Model:* `+"`%s`"+`
• *Effort:* `+"`%s`"+`
• *Host Data Dir:* `+"`%s`",
		sess.ActiveWorkspaceLabel,
		sess.ActiveWorkspaceDir,
		activeConv,
		sess.Model,
		sess.Effort,
		b.cfg.DataDir,
	)

	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdNew(chatID int64, sess *UserSession, args string) {
	sess.mu.Lock()
	sess.ActiveConversationID = ""
	if args != "" {
		folderMap := b.getKnownFolders()
		if fullPath, ok := folderMap[args]; ok {
			sess.ActiveWorkspaceDir = fullPath
			sess.ActiveWorkspaceLabel = args
		} else {
			sess.ActiveWorkspaceDir = args
			sess.ActiveWorkspaceLabel = getFolderLabel(args)
		}
	}
	folderLabel := sess.ActiveWorkspaceLabel
	sess.mu.Unlock()

	reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("✨ Started a new conversation in *%s*. Send your prompt to begin!", folderLabel))
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) getKnownFolders() map[string]string {
	folderMap := make(map[string]string)
	convs, err := b.store.List(context.Background(), store.ListOptions{Limit: 150})
	if err == nil {
		for _, c := range convs {
			if len(c.WorkspaceURIs) > 0 {
				raw := c.WorkspaceURIs[0]
				clean := strings.TrimPrefix(raw, "file://")
				clean = strings.TrimRight(clean, "/")
				label := getFolderLabel(clean)
				if label != "" && clean != "" {
					folderMap[label] = clean
				}
			}
		}
	}

	defaultLabel := getFolderLabel(b.cfg.DefaultWorkspace)
	folderMap[defaultLabel] = b.cfg.DefaultWorkspace
	return folderMap
}

func (b *Bot) cmdFolders(chatID int64) {
	folderMap := b.getKnownFolders()

	var rows [][]tgbotapi.InlineKeyboardButton
	for label := range folderMap {
		btn := tgbotapi.NewInlineKeyboardButtonData("📁 "+label, "fld:"+label)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	reply := tgbotapi.NewMessage(chatID, "📂 *Select Target Workspace Folder:*")
	reply.ParseMode = "Markdown"
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdSetFolder(chatID int64, sess *UserSession, target string) {
	if target == "" {
		b.cmdFolders(chatID)
		return
	}

	folderMap := b.getKnownFolders()
	resolvedPath := target
	resolvedLabel := getFolderLabel(target)

	// Check if target was a folder label
	if fullPath, ok := folderMap[target]; ok {
		resolvedPath = fullPath
		resolvedLabel = target
	}

	sess.mu.Lock()
	sess.ActiveWorkspaceDir = resolvedPath
	sess.ActiveWorkspaceLabel = resolvedLabel
	sess.ActiveConversationID = "" // reset conversation for new folder
	sess.mu.Unlock()

	reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("📁 Switched folder to *%s*. Next message starts a new chat there.", resolvedLabel))
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdConvs(chatID int64) {
	convs, err := b.store.List(context.Background(), store.ListOptions{Limit: 8})
	if err != nil {
		reply := tgbotapi.NewMessage(chatID, "Failed to load conversations: "+err.Error())
		_, _ = b.api.Send(reply)
		return
	}

	if len(convs) == 0 {
		reply := tgbotapi.NewMessage(chatID, "No recent conversations found.")
		_, _ = b.api.Send(reply)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("➕ Start New Conversation", "cnv:__new__"),
	})

	for _, c := range convs {
		title := c.Title
		if title == "" {
			lines := strings.Split(c.Preview, "\n")
			title = lines[0]
		}
		if title == "" {
			title = "Conversation " + c.ConversationID[:8]
		}
		if len(title) > 36 {
			title = title[:33] + "..."
		}
		folder := "General"
		if len(c.WorkspaceURIs) > 0 {
			folder = getFolderLabel(c.WorkspaceURIs[0])
		}

		btnText := fmt.Sprintf("[%s] %s", folder, title)
		btn := tgbotapi.NewInlineKeyboardButtonData(btnText, "cnv:"+c.ConversationID)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	reply := tgbotapi.NewMessage(chatID, "💬 *Recent Conversations (Tap to Resume):*")
	reply.ParseMode = "Markdown"
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdSwitchConv(chatID int64, sess *UserSession, convID string) {
	if convID == "" {
		b.cmdConvs(chatID)
		return
	}

	detail, err := b.store.Get(context.Background(), convID)
	if err != nil {
		reply := tgbotapi.NewMessage(chatID, "Conversation not found: "+convID)
		_, _ = b.api.Send(reply)
		return
	}

	sess.mu.Lock()
	sess.ActiveConversationID = convID
	if detail.Summary != nil && len(detail.Summary.WorkspaceURIs) > 0 {
		clean := strings.TrimPrefix(detail.Summary.WorkspaceURIs[0], "file://")
		sess.ActiveWorkspaceDir = clean
		sess.ActiveWorkspaceLabel = getFolderLabel(clean)
	}
	sess.mu.Unlock()

	title := "Conversation"
	if detail.Summary != nil && detail.Summary.Title != "" {
		title = detail.Summary.Title
	} else if detail.Summary != nil && detail.Summary.Preview != "" {
		title = strings.Split(detail.Summary.Preview, "\n")[0]
	}

	reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Switched to: *%s* (`%s`)\nSend a message to continue.", title, convID[:8]))
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdModels(chatID int64) {
	models, err := b.runner.ListModels(context.Background())
	if err != nil || len(models) == 0 {
		models = []agy.ModelInfo{
			{ID: "gemini-4-argon-medium", Name: "Gemini 4 Argon (Medium)"},
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
			{ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, m := range models {
		btn := tgbotapi.NewInlineKeyboardButtonData(m.Name, "mdl:"+m.ID)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}

	reply := tgbotapi.NewMessage(chatID, "🧠 *Select AI Model:*")
	reply.ParseMode = "Markdown"
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdSetModel(chatID int64, sess *UserSession, modelID string) {
	if modelID == "" {
		b.cmdModels(chatID)
		return
	}
	sess.mu.Lock()
	sess.Model = modelID
	sess.mu.Unlock()

	reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Model set to: `%s`", modelID))
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdSetEffort(chatID int64, sess *UserSession, effort string) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort != "low" && effort != "medium" && effort != "high" {
		rows := [][]tgbotapi.InlineKeyboardButton{
			{
				tgbotapi.NewInlineKeyboardButtonData("⚡ Low", "eff:low"),
				tgbotapi.NewInlineKeyboardButtonData("⚖️ Medium", "eff:medium"),
				tgbotapi.NewInlineKeyboardButtonData("🔬 High", "eff:high"),
			},
		}
		reply := tgbotapi.NewMessage(chatID, "⚡ *Select Reasoning Effort Level:*")
		reply.ParseMode = "Markdown"
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		_, _ = b.api.Send(reply)
		return
	}

	sess.mu.Lock()
	sess.Effort = effort
	sess.mu.Unlock()

	reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Reasoning effort set to: *%s*", effort))
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdUsage(chatID int64) {
	usage, err := b.runner.GetUsage(context.Background())
	if err != nil {
		reply := tgbotapi.NewMessage(chatID, "Failed to retrieve quota usage: "+err.Error())
		_, _ = b.api.Send(reply)
		return
	}

	var sb strings.Builder
	sb.WriteString("📈 *Current Model Quotas & Limits:*\n\n")

	for _, g := range usage.Groups {
		sb.WriteString(fmt.Sprintf("*%s*\n", g.Name))
		for _, bucket := range g.Buckets {
			remPct := bucket.RemainingPercent
			bar := renderProgressBar(remPct)
			sb.WriteString(fmt.Sprintf("• `%s` (%s)\n  Remaining: %.0f%%\n  %s\n", bucket.Name, bucket.Window, remPct, bar))
		}
		sb.WriteString("\n")
	}

	reply := tgbotapi.NewMessage(chatID, sb.String())
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdMCP(chatID int64) {
	servers, err := b.runner.ListMCP(context.Background())
	if err != nil {
		reply := tgbotapi.NewMessage(chatID, "Failed to list MCP tools: "+err.Error())
		_, _ = b.api.Send(reply)
		return
	}

	var sb strings.Builder
	sb.WriteString("🔌 *Connected MCP Servers:*\n\n")
	for _, s := range servers {
		sb.WriteString(fmt.Sprintf("• *%s* (`%s`)\n  Status: %s\n", s.Name, s.Type, s.Status))
	}

	reply := tgbotapi.NewMessage(chatID, sb.String())
	reply.ParseMode = "Markdown"
	_, _ = b.api.Send(reply)
}

func (b *Bot) cmdCancel(chatID int64, sess *UserSession) {
	sess.mu.Lock()
	if sess.CancelFunc != nil {
		sess.CancelFunc()
		sess.CancelFunc = nil
		sess.mu.Unlock()
		reply := tgbotapi.NewMessage(chatID, "⏹ Generation stopped.")
		_, _ = b.api.Send(reply)
		return
	}
	sess.mu.Unlock()

	reply := tgbotapi.NewMessage(chatID, "No active generation to cancel.")
	_, _ = b.api.Send(reply)
}

// handleCallback routes inline keyboard button presses.
func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	sess := b.getSession(chatID)
	data := cb.Data

	ack := tgbotapi.NewCallback(cb.ID, "")
	_, _ = b.api.Request(ack)

	if strings.HasPrefix(data, "fld:") {
		path := strings.TrimPrefix(data, "fld:")
		b.cmdSetFolder(chatID, sess, path)
	} else if strings.HasPrefix(data, "cnv:") {
		convID := strings.TrimPrefix(data, "cnv:")
		if convID == "__new__" {
			b.cmdNew(chatID, sess, "")
		} else {
			b.cmdSwitchConv(chatID, sess, convID)
		}
	} else if strings.HasPrefix(data, "mdl:") {
		modelID := strings.TrimPrefix(data, "mdl:")
		b.cmdSetModel(chatID, sess, modelID)
	} else if strings.HasPrefix(data, "eff:") {
		effort := strings.TrimPrefix(data, "eff:")
		b.cmdSetEffort(chatID, sess, effort)
	}
}

// handleChatMessage runs a turn through agy with live streaming and native media upload.
func (b *Bot) handleChatMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	sess := b.getSession(chatID)

	prompt := msg.Text
	if prompt == "" && msg.Caption != "" {
		prompt = msg.Caption
	}

	// Check for inbound attachments (Photo or Document)
	var attachments []agy.Attachment
	if len(msg.Photo) > 0 {
		// Download largest photo
		largest := msg.Photo[len(msg.Photo)-1]
		localPath, err := b.downloadTelegramFile(largest.FileID, "photo.jpg")
		if err != nil {
			b.logger.Error("failed to download photo from telegram", "error", err)
		} else {
			attachments = append(attachments, agy.Attachment{
				Filename: "photo.jpg",
				MimeType: "image/jpeg",
				Path:     localPath,
			})
			if prompt == "" {
				prompt = "What is shown in this attached image?"
			}
		}
	} else if msg.Document != nil {
		doc := msg.Document
		filename := doc.FileName
		if filename == "" {
			filename = "document"
		}
		localPath, err := b.downloadTelegramFile(doc.FileID, filename)
		if err != nil {
			b.logger.Error("failed to download document from telegram", "error", err)
		} else {
			attachments = append(attachments, agy.Attachment{
				Filename: filename,
				MimeType: doc.MimeType,
				Path:     localPath,
			})
			if prompt == "" {
				prompt = "Analyze this attached file."
			}
		}
	}

	if prompt == "" && len(attachments) == 0 {
		return
	}

	// Send initial "Thinking..." message
	initialMsg, err := b.api.Send(tgbotapi.NewMessage(chatID, "💭 _Thinking..._"))
	if err != nil {
		b.logger.Error("failed to send initial telegram message", "error", err)
		return
	}
	messageID := initialMsg.MessageID

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	sess.mu.Lock()
	sess.CancelFunc = cancel
	convID := sess.ActiveConversationID
	wsDir := sess.ActiveWorkspaceDir
	model := sess.Model
	effort := sess.Effort
	sess.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		sess.CancelFunc = nil
		sess.mu.Unlock()
	}()

	events, err := b.runner.StreamChat(ctx, agy.ChatRequest{
		Prompt:         prompt,
		ConversationID: convID,
		WorkspaceDir:   wsDir,
		Model:          model,
		Effort:         effort,
		Attachments:    attachments,
	})
	if err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ Error: "+err.Error())
		_, _ = b.api.Send(edit)
		return
	}

	var accumulatedText strings.Builder
	var lastEditTime = time.Now()
	var finalConvID string

	for ev := range events {
		if ev.ConversationID != "" {
			finalConvID = ev.ConversationID
		}

		if ev.Event == "step_update" {
			su := ev.StepUpdatePayload()
			if su != nil && su.TextDelta != "" {
				accumulatedText.WriteString(su.TextDelta)

				// Throttle Telegram message edits to ~1.5s to avoid rate limits
				if time.Since(lastEditTime) > 1500*time.Millisecond {
					currentText := accumulatedText.String()
					if len(currentText) > 4000 {
						currentText = currentText[len(currentText)-4000:]
					}
					edit := tgbotapi.NewEditMessageText(chatID, messageID, currentText)
					_, _ = b.api.Send(edit)
					lastEditTime = time.Now()
				}
			}
		} else if ev.Event == "result" {
			if ev.Result != nil {
				if ev.Result.Response != "" {
					accumulatedText.Reset()
					accumulatedText.WriteString(ev.Result.Response)
				}
				if ev.Result.ConversationID != "" {
					finalConvID = ev.Result.ConversationID
				}
			}
		} else if ev.Event == "error" {
			edit := tgbotapi.NewEditMessageText(chatID, messageID, "❌ "+ev.Error)
			_, _ = b.api.Send(edit)
			return
		}
	}

	// Update active conversation in session
	if finalConvID != "" {
		sess.mu.Lock()
		sess.ActiveConversationID = finalConvID
		sess.mu.Unlock()
	}

	// Final Edit with full output
	fullResponse := accumulatedText.String()
	if fullResponse == "" {
		fullResponse = "_Completed with no output text._"
	}

	// Handle Telegram's 4096 char limit
	if len(fullResponse) <= 4000 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fullResponse)
		_, _ = b.api.Send(edit)
	} else {
		// Send initial block
		edit := tgbotapi.NewEditMessageText(chatID, messageID, fullResponse[:4000])
		_, _ = b.api.Send(edit)
		// Send overflow chunks
		for i := 4000; i < len(fullResponse); i += 4000 {
			end := i + 4000
			if end > len(fullResponse) {
				end = len(fullResponse)
			}
			chunk := tgbotapi.NewMessage(chatID, fullResponse[i:end])
			_, _ = b.api.Send(chunk)
		}
	}

	// Native Outbound Media Delivery:
	// Detect any generated photos or documents and dispatch them natively to Telegram!
	mediaItems := agy.ExtractMediaFromText(fullResponse)
	for _, m := range mediaItems {
		if strings.HasPrefix(m.MimeType, "image/") {
			photoMsg := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(m.FilePath))
			photoMsg.Caption = m.Filename
			_, _ = b.api.Send(photoMsg)
		} else {
			docMsg := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(m.FilePath))
			docMsg.Caption = m.Filename
			_, _ = b.api.Send(docMsg)
		}
	}
}

// downloadTelegramFile fetches an inbound photo/document file via Telegram Bot API.
func (b *Bot) downloadTelegramFile(fileID, filename string) (string, error) {
	fileURL, err := b.api.GetFileDirectURL(fileID)
	if err != nil {
		return "", fmt.Errorf("get file direct url: %w", err)
	}

	resp, err := http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("http get telegram file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("telegram file download http %d", resp.StatusCode)
	}

	uploadsDir := filepath.Join(b.cfg.DataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir uploads: %w", err)
	}

	destFilename := fmt.Sprintf("tg_%d_%s", time.Now().UnixNano(), filename)
	destPath := filepath.Join(uploadsDir, destFilename)

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create local upload file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("copy telegram file bytes: %w", err)
	}

	return destPath, nil
}

func getFolderLabel(pathOrURI string) string {
	raw := strings.TrimPrefix(pathOrURI, "file://")
	raw = strings.TrimRight(raw, "/")
	if u, err := url.PathUnescape(raw); err == nil {
		raw = u
	}
	parts := strings.Split(raw, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "General"
}

func renderProgressBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	totalBlocks := 10
	filled := int((pct / 100.0) * float64(totalBlocks))
	return strings.Repeat("█", filled) + strings.Repeat("░", totalBlocks-filled)
}
