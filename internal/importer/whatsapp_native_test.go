package importer

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/whatsappmedia"

	_ "modernc.org/sqlite"
)

func TestRepairLegacyMediaPlaceholders(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "ChatStorage.sqlite")
	waDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open whatsapp db: %v", err)
	}
	defer waDB.Close()
	if _, err := waDB.Exec(`
		CREATE TABLE ZWAMESSAGE (Z_PK INTEGER PRIMARY KEY, ZSTANZAID VARCHAR, ZMEDIAITEM INTEGER);
		CREATE TABLE ZWAMEDIAITEM (Z_PK INTEGER PRIMARY KEY, ZMEDIALOCALPATH VARCHAR);
		INSERT INTO ZWAMEDIAITEM (Z_PK, ZMEDIALOCALPATH) VALUES (7, 'Media/jenn/photo.jpg');
		INSERT INTO ZWAMESSAGE (Z_PK, ZSTANZAID, ZMEDIAITEM) VALUES (1, 'abc123', 7);
	`); err != nil {
		t.Fatalf("seed whatsapp db: %v", err)
	}

	mediaPath := filepath.Join(root, "Message", "Media", "jenn")
	if err := os.MkdirAll(mediaPath, 0o755); err != nil {
		t.Fatalf("mkdir media path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaPath, "photo.jpg"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write media file: %v", err)
	}

	store, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("db.New(): %v", err)
	}
	defer store.Close()
	if err := store.UpsertConversation(&db.Conversation{
		ConversationID: "whatsapp:14699991654@s.whatsapp.net",
		Name:           "Jenn",
		SourcePlatform: "whatsapp",
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	if err := store.UpsertMessage(&db.Message{
		MessageID:      "whatsapp:abc123",
		ConversationID: "whatsapp:14699991654@s.whatsapp.net",
		Body:           "[Photo]",
		TimestampMS:    1,
		SourcePlatform: "whatsapp",
		SourceID:       "abc123",
	}); err != nil {
		t.Fatalf("seed placeholder message: %v", err)
	}

	result, err := (&WhatsAppNative{DBPath: dbPath}).RepairLegacyMediaPlaceholders(store)
	if err != nil {
		t.Fatalf("RepairLegacyMediaPlaceholders(): %v", err)
	}
	if result.MessagesRepaired != 1 {
		t.Fatalf("MessagesRepaired = %d, want 1", result.MessagesRepaired)
	}

	msg, err := store.GetMessageByID("whatsapp:abc123")
	if err != nil {
		t.Fatalf("GetMessageByID(): %v", err)
	}
	if msg == nil {
		t.Fatal("expected repaired message")
	}
	if msg.MimeType != "image/jpeg" {
		t.Fatalf("mime_type = %q, want image/jpeg", msg.MimeType)
	}
	relativePath, err := whatsappmedia.DecodeLocalMediaRef(msg.MediaID)
	if err != nil {
		t.Fatalf("DecodeLocalMediaRef(): %v", err)
	}
	if relativePath != "Media/jenn/photo.jpg" {
		t.Fatalf("relative path = %q", relativePath)
	}
}

func TestInferWhatsAppMediaMIME(t *testing.T) {
	if got := inferWhatsAppMediaMIME("Media/jenn/voice.opus", "[Audio]"); got != "audio/ogg" {
		t.Fatalf("got %q, want audio/ogg", got)
	}
	if got := inferWhatsAppMediaMIME("Media/jenn/photo.jpg", "[Photo]"); !strings.HasPrefix(got, "image/") {
		t.Fatalf("got %q, want image/*", got)
	}
}

func TestRawGroupJIDRe(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"16154856400-1585405251", true},
		{"16154856400-1585405251@g.us", true},
		{"123-456", true},
		{"My Group", false},
		{"", false},
		{"Family", false},
		{"+16154856400", false},
	}
	for _, tc := range cases {
		got := rawGroupJIDRe.MatchString(tc.input)
		if got != tc.want {
			t.Errorf("rawGroupJIDRe.MatchString(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestDeriveGroupName(t *testing.T) {
	cases := []struct {
		name         string
		participants []map[string]string
		want         string
	}{
		{
			name: "display names preferred over phones",
			participants: []map[string]string{
				{"name": "Alice", "number": "+1111"},
				{"name": "+12223334444", "number": "+12223334444"},
				{"name": "Bob", "number": "+2222"},
			},
			want: "Alice, Bob",
		},
		{
			name: "falls back to phone numbers when no display names",
			participants: []map[string]string{
				{"name": "+12223334444", "number": "+12223334444"},
				{"name": "+19998887777", "number": "+19998887777"},
			},
			want: "+12223334444, +19998887777",
		},
		{
			name:         "empty participants",
			participants: nil,
			want:         "",
		},
	}
	for _, tc := range cases {
		got := deriveGroupName(tc.participants)
		if got != tc.want {
			t.Errorf("%s: deriveGroupName() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLoadChatsGroupNameFallback(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "ChatStorage.sqlite")
	waDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open whatsapp db: %v", err)
	}
	defer waDB.Close()

	if _, err := waDB.Exec(`
		CREATE TABLE ZWACHATSESSION (
			Z_PK INTEGER PRIMARY KEY,
			ZCONTACTJID VARCHAR,
			ZPARTNERNAME VARCHAR,
			ZLASTMESSAGEDATE REAL,
			ZREMOVED INTEGER
		);
		CREATE TABLE ZWAGROUPMEMBER (
			Z_PK INTEGER PRIMARY KEY,
			ZCHATSESSION INTEGER,
			ZMEMBERJID VARCHAR,
			ZCONTACTNAME VARCHAR
		);
		CREATE TABLE ZWAMESSAGE (
			Z_PK INTEGER PRIMARY KEY,
			ZSTANZAID VARCHAR,
			ZTEXT VARCHAR,
			ZMESSAGEDATE REAL,
			ZISFROMME INTEGER,
			ZFROMJID VARCHAR,
			ZPUSHNAME VARCHAR,
			ZCHATSESSION INTEGER,
			ZMEDIAITEM INTEGER
		);
		CREATE TABLE ZWAMEDIAITEM (Z_PK INTEGER PRIMARY KEY, ZMEDIALOCALPATH VARCHAR);
		-- Group with no name (ZPARTNERNAME = raw JID base)
		INSERT INTO ZWACHATSESSION VALUES (1, '16154856400-1585405251@g.us', '16154856400-1585405251', 1000, 0);
		-- Group members
		INSERT INTO ZWAGROUPMEMBER VALUES (1, 1, '15551234567@s.whatsapp.net', 'Alice');
		INSERT INTO ZWAGROUPMEMBER VALUES (2, 1, '15559876543@s.whatsapp.net', 'Bob');
		-- A message so the chat is kept
		INSERT INTO ZWAMESSAGE VALUES (1, 'msg1', 'hello', 1000, 0, '15551234567@s.whatsapp.net', '', 1, NULL);
	`); err != nil {
		t.Fatalf("seed whatsapp db: %v", err)
	}

	store, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("db.New(): %v", err)
	}
	defer store.Close()

	importer := &WhatsAppNative{DBPath: dbPath, SinceMS: -1}
	result, err := importer.ImportFromDB(store)
	if err != nil {
		t.Fatalf("ImportFromDB: %v", err)
	}
	if result.ConversationsCreated != 1 {
		t.Fatalf("ConversationsCreated = %d, want 1", result.ConversationsCreated)
	}

	convs, err := store.ListConversations(10)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}

	name := convs[0].Name
	if name == "16154856400-1585405251" || name == "16154856400-1585405251@g.us" {
		t.Errorf("group name is still the raw JID: %q", name)
	}
	if name != "Alice, Bob" {
		t.Errorf("group name = %q, want %q", name, "Alice, Bob")
	}
}
