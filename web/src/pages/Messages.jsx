import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useMessages } from '../context/MessagesContext';
import { useRealtime } from '../context/RealtimeContext';
import { useT } from '../i18n/I18nContext';

export default function Messages() {
  const { profile } = useAuth();
  const { markRead } = useMessages();
  const { subscribe } = useRealtime();
  const { t } = useT();
  const [searchParams, setSearchParams] = useSearchParams();
  const [conversations, setConversations] = useState([]);
  const [activeId, setActiveId] = useState(searchParams.get('conversation'));
  const [messages, setMessages] = useState([]);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState(null);
  const endRef = useRef(null);
  const activeIdRef = useRef(activeId);
  activeIdRef.current = activeId;

  async function loadConversations() {
    try {
      const res = await api.listConversations();
      setConversations(res.items || []);
      if (!activeId && res.items?.length) {
        setActiveId(res.items[0].id);
      }
    } catch (e) {
      setError(e.message);
    }
  }

  async function loadMessages(id) {
    if (!id) return;
    try {
      const res = await api.listMessages(id);
      setMessages(res.items || []);
    } catch (e) {
      setError(e.message);
    }
  }

  useEffect(() => {
    loadConversations();
  }, []);

  useEffect(() => {
    if (!activeId) return undefined;
    loadMessages(activeId);
    setSearchParams({ conversation: activeId }, { replace: true });
    // Opening a thread clears its unread count locally and on the server.
    setConversations((prev) => prev.map((c) => (c.id === activeId ? { ...c, unreadCount: 0 } : c)));
    markRead(activeId);
    // A slow safety poll in case the realtime stream is down; inbound messages
    // normally arrive instantly via the SSE subscription below.
    const id = setInterval(() => loadMessages(activeId), 30000);
    return () => clearInterval(id);
  }, [activeId]);

  // Live updates: when the server pushes a "message" event for the open thread,
  // refresh it immediately (truly realtime, no polling lag). Any message event
  // also refreshes the conversation list so its preview/unread stay current.
  useEffect(
    () =>
      subscribe((update) => {
        if (update.type !== 'message') return;
        loadConversations();
        if (update.conversationId && update.conversationId === activeIdRef.current) {
          loadMessages(activeIdRef.current);
        }
      }),
    [subscribe],
  );

  // Refresh the conversation list periodically so last-message time and unread
  // counts stay current.
  useEffect(() => {
    const id = setInterval(loadConversations, 20000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  async function send(e) {
    e.preventDefault();
    if (!draft.trim() || !activeId) return;
    setError(null);
    try {
      await api.sendMessage(activeId, draft.trim());
      setDraft('');
      loadMessages(activeId);
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="container">
      <h1>{t('msg.title')}</h1>
      {error && <p className="error">{error}</p>}
      {conversations.length === 0 ? (
        <p>{t('msg.none')}</p>
      ) : (
        <div className="messages-layout">
          <div className="conv-list">
            {conversations.map((c) => {
              const role = c.hostId === profile?.id ? t('msg.guestEnquiry') : t('msg.yourEnquiry');
              return (
                <div
                  key={c.id}
                  className={`conv-item${c.id === activeId ? ' active' : ''}`}
                  onClick={() => setActiveId(c.id)}
                >
                  <div className="conv-row">
                    <span>{role}</span>
                    {c.unreadCount > 0 && <span className="count-badge">{c.unreadCount}</span>}
                  </div>
                  <small>{c.lastMessageAt ? new Date(c.lastMessageAt).toLocaleString() : ''}</small>
                </div>
              );
            })}
          </div>

          <div className="thread">
            <div className="thread-messages">
              {messages.map((m) => (
                <div key={m.id} className={`bubble ${m.senderId === profile?.id ? 'mine' : 'theirs'}`}>
                  {m.body}
                  <small>{new Date(m.createdAt).toLocaleTimeString()}</small>
                </div>
              ))}
              <div ref={endRef} />
            </div>
            <form className="thread-composer" onSubmit={send}>
              <input
                placeholder={t('msg.write')}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
              />
              <button className="btn btn-primary" type="submit">{t('msg.send')}</button>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
