import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';

export default function Messages() {
  const { profile } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();
  const [conversations, setConversations] = useState([]);
  const [activeId, setActiveId] = useState(searchParams.get('conversation'));
  const [messages, setMessages] = useState([]);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState(null);
  const endRef = useRef(null);

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
    loadMessages(activeId);
    if (activeId) setSearchParams({ conversation: activeId }, { replace: true });
  }, [activeId]);

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
      <h1>Messages</h1>
      {error && <p className="error">{error}</p>}
      {conversations.length === 0 ? (
        <p>No conversations yet. Start one from a listing’s “Contact host” button.</p>
      ) : (
        <div className="messages-layout">
          <div className="conv-list">
            {conversations.map((c) => {
              const role = c.hostId === profile?.id ? 'Guest enquiry' : 'Your enquiry';
              return (
                <div
                  key={c.id}
                  className={`conv-item${c.id === activeId ? ' active' : ''}`}
                  onClick={() => setActiveId(c.id)}
                >
                  <div>{role}</div>
                  <small>{new Date(c.lastMessageAt).toLocaleString()}</small>
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
                placeholder="Write a message…"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
              />
              <button className="btn btn-primary" type="submit">Send</button>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
