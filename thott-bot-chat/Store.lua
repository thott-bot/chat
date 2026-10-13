local _, T = ...

local Store = {
    received = false,
    codex = "offline",
    note = "",
    starting = false,
    models = {},
    projects = {},
    syncedAt = 0,
}
T.Store = Store

local ACK_WINDOW = 2
local RANK = { needs_you = 0, error = 0, active = 1, queued = 1, idle = 2, history = 2 }

local remote, locals, byId = {}, {}, {}
local outbox, answered, readAt, renamed = {}, {}, {}, {}
local unacked, sentAt, acked = {}, {}, {}
local rev

local function index()
    wipe(byId)
    for _, list in ipairs({ remote, locals }) do
        for _, thread in ipairs(list) do
            byId[thread.id] = thread
        end
    end
end

function Store.Rank(thread)
    if thread.id:find("^local:") and not Store.Pending(thread) then
        return 2
    end
    return RANK[thread.state] or 2
end

function Store.Find(id)
    return id and byId[id]
end

function Store.Threads()
    local threads = {}
    for _, list in ipairs({ locals, remote }) do
        for _, thread in ipairs(list) do
            table.insert(threads, thread)
        end
    end
    table.sort(threads, function(a, b)
        local ra, rb = Store.Rank(a), Store.Rank(b)
        if ra ~= rb then
            return ra < rb
        end
        return (a.updated or 0) > (b.updated or 0)
    end)
    return threads
end

function Store.Waiting(except)
    local count = 0
    for _, thread in pairs(byId) do
        if thread.id ~= except and Store.Rank(thread) == 0 then
            count = count + 1
        end
    end
    return count
end

function Store.Awaiting()
    if next(unacked) or Store.codex == "error" or Store.starting then
        return true
    end
    for _, thread in pairs(byId) do
        if Store.Rank(thread) == 1 then
            return true
        end
    end
    return false
end

function Store.Track(seq)
    unacked[seq] = true
end

function Store.Tracked(seq)
    return unacked[seq] == true
end

T.On("PacketSent", function(seq)
    sentAt[seq] = GetTime()
end)

function Store.Delivery(seq)
    if T.Transport.Sending(seq) then
        return "sending"
    elseif acked[seq] then
        return "delivered"
    elseif sentAt[seq] and Store.syncedAt - sentAt[seq] >= ACK_WINDOW then
        return "failed"
    end
    return "sent"
end

function Store.Turns(thread)
    local turns = {}
    for _, turn in ipairs(thread.turns or {}) do
        table.insert(turns, turn)
    end
    for _, message in ipairs(outbox[thread.id] or {}) do
        table.insert(turns, { role = "user", text = message.text, seq = message.seq, pending = true })
    end
    return turns
end

function Store.Pending(thread)
    for _, message in ipairs(outbox[thread.id] or {}) do
        if Store.Delivery(message.seq) ~= "failed" then
            return true
        end
    end
    return false
end

function Store.AddReply(thread, text, seq)
    outbox[thread.id] = outbox[thread.id] or {}
    table.insert(outbox[thread.id], { text = text, seq = seq, at = time() })
    T.Fire("StoreChanged", {})
end

function Store.Retract(thread, seq)
    for i, message in ipairs(outbox[thread.id] or {}) do
        if message.seq == seq then
            table.remove(outbox[thread.id], i)
            return message.text
        end
    end
end

-- The app limits conversation names to 120 characters. Count UTF-8 lead bytes (continuation bytes are 0x80-0xBF),
-- so the cut never splits a character.
local function shorten(text)
    local count = 0
    for index = 1, #text do
        local byte = text:byte(index)
        if byte < 0x80 or byte > 0xBF then
            count = count + 1
            if count > 120 then
                return text:sub(1, index - 1)
            end
        end
    end
    return text
end

function Store.AddTask(request, seq)
    local thread = {
        id = "local:" .. seq,
        agent = request.agent,
        origin = T.Transport.session .. ":" .. seq,
        title = shorten(strtrim((request.text:gsub("%s+", " ")))),
        state = "queued",
        project = request.project,
        cwd = request.cwd,
        model = request.model,
        effort = request.effort,
        updated = time(),
        turns = {},
    }
    outbox[thread.id] = { { text = request.text, seq = seq, at = time() } }
    table.insert(locals, 1, thread)
    index()
    T.Fire("StoreChanged", {})
    return thread
end

function Store.Reissue(thread, text, seq)
    thread.origin = T.Transport.session .. ":" .. seq
    thread.updated = time()
    outbox[thread.id] = { { text = text, seq = seq, at = time() } }
    T.Fire("StoreChanged", {})
end

function Store.Answer(thread, approval)
    answered[thread.id] = approval
    T.Fire("StoreChanged", {})
end

function Store.Answered(thread)
    return thread.approval ~= nil and answered[thread.id] == thread.approval.id
end

function Store.Title(thread)
    return renamed[thread.id] or thread.title
end

function Store.Rename(thread, title)
    renamed[thread.id] = title
    T.Fire("StoreChanged", {})
end

function Store.IsUnread(thread)
    return thread.unread and readAt[thread.id] ~= thread.updated
end

function Store.MarkRead(thread)
    if Store.IsUnread(thread) then
        readAt[thread.id] = thread.updated
        return true
    end
    return false
end

local function prefix(text)
    return strtrim(text or ""):sub(1, 1000)
end

local function listed(thread, message)
    for _, turn in ipairs(thread.turns or {}) do
        if turn.role == "user" and prefix(turn.text) == prefix(message.text) and (not turn.at or not message.at or turn.at >= message.at) then
            return true
        end
    end
    return false
end

local function reconcile(thread)
    local pending = outbox[thread.id]
    if not pending then
        return
    end
    for i = #pending, 1, -1 do
        if listed(thread, pending[i]) then
            table.remove(pending, i)
        end
    end
    if #pending == 0 then
        outbox[thread.id] = nil
    end
end

local function adopt(thread)
    for i = #locals, 1, -1 do
        local task = locals[i]
        if thread.origin and thread.origin == task.origin then
            outbox[thread.id] = outbox[task.id]
            outbox[task.id] = nil
            table.remove(locals, i)
            T.Fire("ThreadReplaced", task.id, thread.id)
        end
    end
end

local function lastReply(thread)
    local at = 0
    for _, turn in ipairs(thread.turns or {}) do
        if turn.role == "assistant" and (turn.at or 0) > at then
            at = turn.at
        end
    end
    return at
end

function Store.Apply(data)
    if type(data) ~= "table" then
        return
    end
    Store.syncedAt = GetTime()
    local initial = not Store.received
    Store.received = true
    for _, seq in ipairs((data.acks or {})[T.Transport.session] or {}) do
        acked[seq] = true
    end
    for seq in pairs(unacked) do
        if acked[seq] then
            unacked[seq], sentAt[seq] = nil, nil
        elseif Store.Delivery(seq) == "failed" then
            unacked[seq] = nil
        end
    end
    if data.rev == rev then
        T.Fire("StoreChanged", {})
        return
    end
    rev = data.rev
    Store.codex = data.codex or "offline"
    Store.note = data.note or ""
    Store.starting = data.starting == true
    Store.models = data.models or {}
    Store.projects = data.projects or {}

    local before = {}
    for _, thread in ipairs(remote) do
        before[thread.id] = { state = thread.state, reply = lastReply(thread) }
    end
    remote = data.threads or {}

    local change = { needs = {}, replied = {}, initial = initial }
    for _, thread in ipairs(remote) do
        local was = before[thread.id]
        adopt(thread)
        reconcile(thread)
        if Store.Rank(thread) == 0 and (not was or was.state ~= thread.state) then
            table.insert(change.needs, thread)
        elseif was and not initial and lastReply(thread) > was.reply and Store.Rank(thread) == 2 then
            table.insert(change.replied, thread)
        end
        if renamed[thread.id] == thread.title then
            renamed[thread.id] = nil
        end
        if answered[thread.id] and not Store.Answered(thread) then
            answered[thread.id] = nil
        end
    end
    index()
    T.Fire("StoreChanged", change)
end
