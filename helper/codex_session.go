package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	openElsewhere    = "Open in the Codex app. Reply there, or close it there first."
	turnsPerFetch    = 8
	threadsWithTurns = 10
)

var quietMethods = []string{
	"item/agentMessage/delta", "item/plan/delta", "item/reasoning/textDelta",
	"item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded",
	"item/commandExecution/outputDelta", "item/fileChange/outputDelta",
	"turn/diff/updated",
}

func (s *session) renameThread(p packet) {
	title := trimText(oneLine(p.Title), 120)
	if title == "" || p.Thread == "" {
		return
	}
	err := s.client.call("thread/name/set", map[string]string{"threadId": p.Thread, "name": title}, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.threads[p.Thread]; t != nil {
		if err != nil {
			t.err = oneLine("Couldn't rename the conversation: " + err.Error())
		} else {
			t.info.Name = title
			t.err = ""
		}
		s.dirty = true
	}
	if err != nil {
		logf("Couldn't rename Codex thread %s: %v", p.Thread, err)
	}
}

func (s *session) create(p packet) {
	origin := fmt.Sprintf("%s:%d", p.Session, int64(p.Seq))
	params := map[string]any{}
	if p.Cwd != "" {
		params["cwd"] = p.Cwd
	}
	if p.Model != "" {
		params["model"] = p.Model
	}
	var r struct {
		Thread cxThread `json:"thread"`
	}
	if err := s.client.call("thread/start", params, &r); err != nil {
		logf("Couldn't start a thread for %s: %v", origin, err)
		s.failTask(p, "codex", "Codex couldn't start the task: "+err.Error())
		return
	}
	id := r.Thread.ID
	s.mu.Lock()
	s.origins[id] = origin
	s.upsert(r.Thread).origin = origin
	s.order = append([]string{id}, slices.DeleteFunc(s.order, func(o string) bool { return o == id })...)
	s.mu.Unlock()
	logf("Started thread %s in %s for %s", id, r.Thread.Cwd, origin)
	s.startTurn(id, p)
}

func (s *session) reply(p packet) {
	s.mu.Lock()
	t := s.threads[p.Thread]
	loaded := t != nil && t.info.Status.Type != "notLoaded" && t.info.Status.Type != ""
	s.mu.Unlock()
	if !loaded {
		var r struct {
			Thread cxThread `json:"thread"`
		}
		err := s.client.call("thread/resume", map[string]any{"threadId": p.Thread, "excludeTurns": true}, &r)
		s.mu.Lock()
		s.dirty = true
		if err != nil {

			if t != nil {
				t.err = openElsewhere
			}
		} else {
			s.upsert(r.Thread)
		}
		s.mu.Unlock()
		if err != nil {
			logf("Couldn't resume thread %s: %v", p.Thread, err)
			return
		}
		logf("Resumed thread %s", p.Thread)
	}
	s.startTurn(p.Thread, p)
}

func (s *session) startTurn(id string, p packet) {
	input := []map[string]string{{"type": "text", "text": prompt(p)}}
	params := map[string]any{"threadId": id, "input": input}
	if p.Effort != "" {
		params["effort"] = p.Effort
	}
	var r struct {
		Turn cxTurn `json:"turn"`
	}
	err := s.client.call("turn/start", params, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listWanted = true
	s.dirty = true
	t := s.threads[id]
	if t == nil {
		return
	}
	t.stale = true
	t.readAt = time.Now().Unix()
	if err != nil {
		t.err = oneLine("Codex couldn't start the turn: " + err.Error())
		logf("Couldn't start a turn in %s: %v", id, err)
		return
	}
	t.err = ""
	if r.Turn.ID != "" && (t.live == nil || t.live.ID != r.Turn.ID) {
		t.activeTurn, t.since = r.Turn.ID, r.Turn.StartedAt
		t.live = &cxTurn{ID: r.Turn.ID, Status: r.Turn.Status, StartedAt: r.Turn.StartedAt}
	}
	if p.Effort != "" {
		t.info.ReasoningEffort = p.Effort
	}
	if p.Model != "" {
		t.info.Model = p.Model
	}
	logf("Sent the player's message to thread %s", id)
}

func (s *session) answer(p packet, allow bool) {
	s.mu.Lock()
	var a *approval
	s.approvals = slices.DeleteFunc(s.approvals, func(x *approval) bool {
		if x.id == p.Approval && x.thread == p.Thread {
			a = x
		}
		return x == a
	})
	s.dirty = s.dirty || a != nil
	s.mu.Unlock()
	if a == nil {
		logf("Approval %s is no longer pending", p.Approval)
		return
	}
	decision := "decline"
	if allow {
		decision = "accept"
	}
	if err := a.client.respond(a.rpcID, map[string]string{"decision": decision}); err != nil {
		logf("Couldn't answer approval %s: %v", a.id, err)
		return
	}
	logf("Player chose %s for approval %s in thread %s", decision, a.id, a.thread)
}

func (s *session) stopTurn(p packet) {
	s.mu.Lock()
	turn := ""
	if t := s.threads[p.Thread]; t != nil {
		turn = t.activeTurn
	}
	s.mu.Unlock()
	if turn == "" {
		var r struct {
			Data []cxTurn `json:"data"`
		}
		params := map[string]any{"threadId": p.Thread, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}
		if s.client.call("thread/turns/list", params, &r) == nil && len(r.Data) == 1 && r.Data[0].Status == "inProgress" {
			turn = r.Data[0].ID
		}
	}
	if turn == "" {
		logf("Nothing is running in thread %s", p.Thread)
		return
	}
	if err := s.client.call("turn/interrupt", map[string]any{"threadId": p.Thread, "turnId": turn}, nil); err != nil {
		logf("Couldn't stop thread %s: %v", p.Thread, err)
		return
	}
	logf("Stopped turn %s in thread %s", turn, p.Thread)
}

func (s *session) ready() bool {
	return s.client != nil && s.view().codex == "ready"
}

func (s *session) setCodex(state, problem string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = s.dirty || s.codex != state || s.codexErr != problem
	s.codex, s.codexErr = state, problem
}

func (s *session) superviseCodex(now time.Time) {
	if c := s.client; c != nil {
		if !c.exited() {
			s.mu.Lock()
			recheck := s.accountWanted || s.codex == "signed-out" && now.Sub(s.lastAccount) >= 30*time.Second
			s.accountWanted = false
			s.mu.Unlock()
			if recheck {
				s.checkAccount(now)
			}
			return
		}
		reason := c.exitReason()
		logf("Codex stopped: %s", reason)
		s.client = nil
		s.retryLater(now, now.Sub(c.started) > time.Minute)
		s.mu.Lock()
		s.codex, s.codexErr = "error", "Codex stopped: "+reason
		s.dirty = true
		s.approvals = nil
		for _, t := range s.threads {
			t.info.Status, t.activeTurn, t.activity, t.fileChanges, t.live = cxStatus{Type: "notLoaded"}, "", "", nil, nil
		}
		s.mu.Unlock()
		return
	}
	if now.Before(s.nextStart) {
		return
	}
	path := s.findAgent("codex")
	if path == "" {
		s.nextStart = now.Add(30 * time.Second)
		if s.view().codex != "missing" {
			logf("Codex isn't installed: not on PATH or in the usual install folders")
		}
		s.setCodex("missing", "")
		return
	}
	c, err := startCodex(s.ctx, path, s.notify, s.request)
	if err == nil {
		params := map[string]any{
			"clientInfo":   map[string]string{"name": "thott_bot_chat", "title": "thott.bot chat", "version": version},
			"capabilities": map[string]any{"optOutNotificationMethods": quietMethods},
		}
		if err = c.call("initialize", params, nil); err == nil {
			err = c.notify("initialized")
		}
		if err != nil {
			c.close()
			err = fmt.Errorf("%v (%s)", err, c.exitReason())
		}
	}
	if err != nil {
		logf("Couldn't start Codex %s: %v", path, err)
		s.retryLater(now, false)
		s.setCodex("error", "Couldn't start Codex: "+err.Error())
		return
	}
	logf("Started the Codex app server %s (pid %d)", path, c.cmd.Process.Pid)
	s.client = c
	s.setCodex("error", "")
	s.checkAccount(now)
}

func (s *session) retryLater(now time.Time, reset bool) {
	if reset {
		s.backoff = 0
	}
	s.backoff = min(max(2*s.backoff, 2*time.Second), time.Minute)
	s.nextStart = now.Add(s.backoff)
}

func (s *session) checkAccount(now time.Time) {
	s.lastAccount = now
	var r struct {
		Account            *json.RawMessage `json:"account"`
		RequiresOpenaiAuth bool             `json:"requiresOpenaiAuth"`
	}
	state := "ready"
	if err := s.client.call("account/read", struct{}{}, &r); err != nil {
		logf("Couldn't read the Codex account: %v", err)
	} else if r.Account == nil && r.RequiresOpenaiAuth {
		state = "signed-out"
	}
	previous := s.view().codex
	s.setCodex(state, "")
	if state == previous {
		return
	}
	logf("Codex is %s", state)
	if state == "ready" {
		s.loadModels()
		s.loadServerNames("")
		s.lastList = time.Time{}
	} else {
		s.mu.Lock()
		s.models = nil
		s.dirty = true
		s.mu.Unlock()
	}
}

func (s *session) loadModels() {
	var models []payloadModel
	cursor := ""
	for range 5 {
		params := map[string]any{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var r struct {
			Data []struct {
				ID                        string `json:"id"`
				DisplayName               string `json:"displayName"`
				IsDefault                 bool   `json:"isDefault"`
				DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := s.client.call("model/list", params, &r); err != nil {
			logf("Couldn't list Codex models: %v", err)
			return
		}
		for _, m := range r.Data {
			efforts := []string{}
			for _, e := range m.SupportedReasoningEfforts {
				efforts = append(efforts, e.ReasoningEffort)
			}
			models = append(models, payloadModel{ID: m.ID, Label: m.DisplayName, Default: m.IsDefault, Effort: m.DefaultReasoningEffort, Efforts: efforts})
		}
		if cursor = r.NextCursor; cursor == "" {
			break
		}
	}
	s.mu.Lock()
	s.models = models
	s.dirty = true
	s.mu.Unlock()
}

func (s *session) refresh(now time.Time) {
	if !s.ready() {
		return
	}
	s.mu.Lock()
	mcp := slices.Collect(maps.Keys(s.mcpWanted))
	clear(s.mcpWanted)
	wanted := s.listWanted
	s.listWanted = false
	s.mu.Unlock()
	for _, id := range mcp {
		s.loadServerNames(id)
	}
	if wanted || now.Sub(s.lastList) >= 30*time.Second {
		s.lastList = now
		var r struct {
			Data []cxThread `json:"data"`
		}
		params := map[string]any{"limit": maxThreads, "sortKey": "updated_at", "sortDirection": "desc"}
		if err := s.client.call("thread/list", params, &r); err != nil {
			logf("Couldn't list Codex threads: %v", err)
			s.mu.Lock()
			s.listed, s.dirty = true, true
			s.mu.Unlock()
		} else {
			s.mu.Lock()
			s.order = s.order[:0]
			s.listed, s.dirty = true, true
			listed := map[string]bool{}
			for _, info := range r.Data {
				s.upsert(info)
				s.order = append(s.order, info.ID)
				listed[info.ID] = true
			}

			for id, t := range s.threads {
				if !listed[id] && t.live != nil && t.info.Status.Type != "notLoaded" {
					s.order = append(s.order, id)
				}
			}
			slices.SortStableFunc(s.order, func(a, b string) int {
				return cmp.Compare(s.threads[b].info.UpdatedAt, s.threads[a].info.UpdatedAt)
			})
			s.order = s.order[:min(len(s.order), maxThreads)]
			for id, t := range s.threads {
				if !slices.Contains(s.order, id) && t.activeTurn == "" && !slices.ContainsFunc(s.approvals, func(a *approval) bool { return a.thread == id }) {
					delete(s.threads, id)
				}
			}
			s.mu.Unlock()
		}
	}
	for _, id := range s.turnsDue(now) {
		s.fetchTurns(id, now)
	}
}

func (s *session) turnsDue(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []string
	for _, id := range s.order[:min(len(s.order), threadsWithTurns)] {
		t := s.threads[id]
		if (t.stale || t.fetched != t.info.UpdatedAt) && now.UnixMilli()-t.fetchedAt >= 2000 {
			t.stale = false
			due = append(due, id)
		}
	}
	return due
}

func (s *session) fetchTurns(id string, now time.Time) {
	s.mu.Lock()
	updated := s.threads[id].info.UpdatedAt
	s.mu.Unlock()
	turns, err := s.loadTurns(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.threads[id]
	t.fetched, t.fetchedAt = updated, now.UnixMilli()
	if err != nil {
		logf("Couldn't read turns of thread %s: %v", id, err)
		return
	}
	t.setTurns(turns)
	s.dirty = true
	if n := len(turns); n > 0 && turns[n-1].Status == "inProgress" && t.since == 0 {
		t.since = turns[n-1].StartedAt
	}
}

func (s *session) loadTurns(id string) ([]cxTurn, error) {
	var r struct {
		Data []cxTurn `json:"data"`
	}
	params := map[string]any{"threadId": id, "limit": turnsPerFetch, "sortDirection": "desc", "itemsView": "full"}
	err := s.client.call("thread/turns/list", params, &r)
	if err == nil {
		slices.Reverse(r.Data)
		return r.Data, nil
	}

	var full struct {
		Thread struct {
			Turns []cxTurn `json:"turns"`
		} `json:"thread"`
	}
	if s.client.call("thread/read", map[string]any{"threadId": id, "includeTurns": true}, &full) != nil {
		return nil, err
	}
	turns := full.Thread.Turns
	return turns[max(0, len(turns)-turnsPerFetch):], nil
}

func (s *session) upsert(info cxThread) *threadState {
	s.dirty = true
	t := s.threads[info.ID]
	if t == nil {
		t = &threadState{readAt: info.UpdatedAt, origin: s.origins[info.ID], serverNames: s.serverNames}
		s.threads[info.ID] = t
	}
	t.info = info
	return t
}

type notification struct {
	ThreadID   string          `json:"threadId"`
	TurnID     string          `json:"turnId"`
	Thread     cxThread        `json:"thread"`
	Status     cxStatus        `json:"status"`
	Turn       cxTurn          `json:"turn"`
	Item       cxItem          `json:"item"`
	ThreadName *string         `json:"threadName"`
	RequestID  json.RawMessage `json:"requestId"`
	WillRetry  bool            `json:"willRetry"`
	TokenUsage struct {
		Last struct {
			TotalTokens int64 `json:"totalTokens"`
		} `json:"last"`
		ModelContextWindow *int64 `json:"modelContextWindow"`
	} `json:"tokenUsage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (s *session) notify(method string, params json.RawMessage) {
	if method == "mcpServer/startupStatus/updated" {
		var n struct {
			ThreadID string `json:"threadId"`
			Status   string `json:"status"`
		}
		if json.Unmarshal(params, &n) == nil && n.Status == "ready" {
			s.mu.Lock()
			s.mcpWanted[n.ThreadID] = true
			s.mu.Unlock()
		}
		return
	}
	switch method {
	case "thread/started", "thread/archived", "thread/deleted", "thread/unarchived", "account/updated",
		"serverRequest/resolved", "error", "thread/status/changed", "thread/name/updated", "thread/closed",
		"turn/started", "turn/completed", "item/started", "item/completed", "thread/tokenUsage/updated":
	default:
		return
	}
	var n notification
	json.Unmarshal(params, &n)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
	switch method {
	case "thread/started":
		s.upsert(n.Thread)
		s.listWanted = true
		return
	case "thread/archived", "thread/deleted", "thread/unarchived":
		s.listWanted = true
		return
	case "account/updated":
		s.accountWanted = true
		return
	case "serverRequest/resolved":
		s.approvals = slices.DeleteFunc(s.approvals, func(a *approval) bool { return string(a.rpcID) == string(n.RequestID) })
		return
	case "error":
		if !n.WillRetry {
			logf("Codex error in thread %s: %s", n.ThreadID, n.Error.Message)
		}
		return
	}
	t := s.threads[n.ThreadID]
	if t == nil {
		s.listWanted = true
		return
	}
	switch method {
	case "thread/status/changed":
		t.info.Status = n.Status
	case "thread/name/updated":
		if n.ThreadName != nil {
			t.info.Name = *n.ThreadName
		}
	case "thread/closed":
		t.info.Status = cxStatus{Type: "notLoaded"}
	case "turn/started":
		t.activeTurn, t.since = n.Turn.ID, n.Turn.StartedAt
		t.live = &cxTurn{ID: n.Turn.ID, Status: n.Turn.Status, StartedAt: n.Turn.StartedAt}
	case "turn/completed":

		if t.live != nil && t.live.ID == n.Turn.ID {
			t.live.Status, t.live.CompletedAt, t.live.Error = n.Turn.Status, n.Turn.CompletedAt, n.Turn.Error
		}
		t.activeTurn, t.since, t.activity, t.fileChanges, t.stale = "", 0, "", nil, true
		s.listWanted = true
		s.approvals = slices.DeleteFunc(s.approvals, func(a *approval) bool { return a.thread == n.ThreadID })
	case "item/started":
		if d := describe(n.Item, t.serverNames); d != "" {
			t.activity = d
		}
		if n.Item.Type == "fileChange" && len(n.Item.Changes) > 0 {
			var names []string
			for _, c := range n.Item.Changes {
				names = append(names, relPath(c.Path, t.info.Cwd))
			}
			if t.fileChanges == nil {
				t.fileChanges = map[string]string{}
			}
			t.fileChanges[n.Item.ID] = "Change " + strings.Join(names, ", ")
		}
	case "item/completed":
		t.addLive(n.TurnID, n.Item)
		delete(t.fileChanges, n.Item.ID)
	case "thread/tokenUsage/updated":
		if w := n.TokenUsage.ModelContextWindow; w != nil {
			left := contextLeft(n.TokenUsage.Last.TotalTokens, *w)
			t.contextLeft = &left
		}
	}
}

func (s *session) request(c *codexClient, id json.RawMessage, method string, params json.RawMessage) {
	var p struct {
		ThreadID string `json:"threadId"`
		ItemID   string `json:"itemId"`
		Reason   string `json:"reason"`
		Command  string `json:"command"`
	}
	json.Unmarshal(params, &p)
	a := &approval{id: fmt.Sprintf("%d-%s", c.id, strings.Trim(string(id), `"`)), rpcID: id, client: c, thread: p.ThreadID, reason: p.Reason}
	switch method {
	case "item/commandExecution/requestApproval":
		a.kind, a.text = "command", firstOf(p.Command, "Run a command")
	case "item/fileChange/requestApproval":
		s.mu.Lock()
		if t := s.threads[p.ThreadID]; t != nil {
			a.text = t.fileChanges[p.ItemID]
		}
		s.mu.Unlock()
		a.kind, a.text = "file", firstOf(a.text, "Change files")
	default:
		logf("Declined Codex request %s: the addon can't answer it", method)
		c.respondError(id, "thott.bot chat can't answer "+method)
		return
	}
	s.mu.Lock()
	s.approvals = append(s.approvals, a)
	s.dirty = true
	s.mu.Unlock()
	logf("Codex asks the player to approve %s in thread %s", a.id, a.thread)
}
