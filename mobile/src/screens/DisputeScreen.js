import { useCallback, useEffect, useState } from 'react';
import { ScrollView, View, Text, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';

// DisputeScreen is the per-case detail surface of the Resolution Center.
// Guests/openers come here from TripsScreen by tapping the case badge;
// hosts come here from HostPropertyBookingsScreen (via the same id). Both
// parties can:
//   - Read the case kind, reason, requested amount, status and SLA dueAt
//   - See the host response (if posted) and every piece of evidence
//   - Add new evidence (URL + note); the API allows both opener and host
//   - Post a host response (host/admin only; gated client-side by role,
//     enforced server-side by the application service)
//
// The screen does not surface admin decision controls — those live in the
// admin web queue. The mobile app is for the two parties only.
export default function DisputeScreen({ route, navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const id = route?.params?.id;
  const [d, setD] = useState(null);
  const [me, setMe] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [evDraft, setEvDraft] = useState({ url: '', note: '' });
  const [hostDraft, setHostDraft] = useState('');
  // S129: free-text reply composer with an optional comma-separated list of
  // photo evidence URLs. Kept separate from evDraft because submit semantics
  // differ — a reply may fan out to several backend evidence rows.
  const [replyDraft, setReplyDraft] = useState({ body: '', urlsRaw: '' });
  const [replyBusy, setReplyBusy] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const [d, u] = await Promise.all([api.getDispute(id), api.me().catch(() => null)]);
      setD(d);
      setMe(u);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, id]);

  useEffect(() => {
    if (authenticated) load();
    else setLoading(false);
  }, [authenticated, load]);

  async function submitEvidence() {
    if (!evDraft.url.trim() && !evDraft.note.trim()) {
      setError('Add a URL or a note before submitting.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const next = await api.addDisputeEvidence(id, {
        url: evDraft.url.trim(),
        note: evDraft.note.trim(),
      });
      setD(next);
      setEvDraft({ url: '', note: '' });
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function submitHostResponse() {
    if (!hostDraft.trim()) {
      setError('Write a response first.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const next = await api.hostRespondDispute(id, hostDraft.trim());
      setD(next);
      setHostDraft('');
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  // submitReply (S129) posts a free-text reply with optional photo URLs to
  // an open / under_review case. The backend has no batched /replies route,
  // so the API wrapper fans out across POST /disputes/:id/evidence — one
  // row per URL, or one note-only row when there are no URLs.
  // We optimistically prepend a synthetic timeline entry so the user sees
  // their reply immediately, then refetch via load() to reconcile with the
  // server (which assigns real ids/timestamps and surfaces any moderation
  // side-effects).
  async function submitReply() {
    const body = replyDraft.body.trim();
    if (!body) return; // submit button is also disabled, this is belt-and-braces
    const photoUrls = replyDraft.urlsRaw
      .split(',')
      .map((u) => u.trim())
      .filter(Boolean);
    setReplyBusy(true);
    setError(null);
    // Optimistic prepend — synthesize one timeline entry per URL (or one
    // note-only entry). Real ids and addedAt arrive on the refetch below.
    const now = new Date().toISOString();
    const optimistic = photoUrls.length === 0
      ? [{ id: `optimistic-${Date.now()}`, url: '', note: body, addedAt: now, optimistic: true }]
      : photoUrls.map((u, i) => ({
          id: `optimistic-${Date.now()}-${i}`,
          url: u,
          note: body,
          addedAt: now,
          optimistic: true,
        }));
    setD((prev) => prev && { ...prev, evidence: [...optimistic, ...(prev.evidence || [])] });
    try {
      await api.postDisputeReply(id, { body, photoUrls });
      // Refetch the dispute so the timeline reflects the canonical server
      // state (real ids + ordering); load() also clears any stale error.
      await load();
      setReplyDraft({ body: '', urlsRaw: '' });
    } catch (e) {
      // Roll back the optimistic prepend on failure.
      setD((prev) => prev && {
        ...prev,
        evidence: (prev.evidence || []).filter((ev) => !ev.optimistic),
      });
      setError(e.message);
    } finally {
      setReplyBusy(false);
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        <Text style={styles.meta}>Sign in to view this case.</Text>
        <Pressable style={styles.btn} onPress={login}><Text style={styles.btnText}>Sign in</Text></Pressable>
      </View>
    );
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;
  if (!d) return <View style={styles.center}><Text style={styles.error}>{error || 'Case not found'}</Text></View>;

  const isOpener = me && me.id === d.openerId;
  const isHostRole = me && (me.role === 'host' || me.role === 'admin');
  const isTerminal = d.status === 'resolved' || d.status === 'rejected';
  const canAddEvidence = !isTerminal;
  // Host-side controls: hosts can post a response while the case is open and
  // before they've already responded. We do NOT auto-detect "this is your
  // listing"; the backend gates the action by listing ownership and returns
  // 403 otherwise, surfaced as a normal error.
  const canHostRespond = isHostRole && !isOpener && !isTerminal && !d.hostResponse;
  // canReply (S129): both parties (guest opener AND host) can post a reply
  // while the case is still active. We treat under_review the same as open
  // because moderators may want to see additional context from either side
  // before deciding. Terminal cases (resolved/rejected) are read-only.
  const canReply = d.status === 'open' || d.status === 'under_review';
  const amountStr = d.requestedAmountCents > 0
    ? `${(d.requestedAmountCents / 100).toFixed(2)} ${d.currency}`
    : '—';

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 24 }}>
      {error && <Text style={styles.error} accessibilityRole="alert">{error}</Text>}

      <View style={styles.header}>
        <Text style={styles.title} accessibilityRole="header">Case · {d.kind}</Text>
        <View style={styles.badges} accessibilityLabel={`Status: ${d.status}${d.overdue ? ', overdue' : ''}`}>
          <Text
            style={[styles.badge, statusStyles[d.status] || statusStyles.open]}
            accessibilityElementsHidden
            importantForAccessibility="no"
          >{d.status}</Text>
          {d.overdue && (
            <Text
              style={[styles.badge, statusStyles.overdue]}
              accessibilityElementsHidden
              importantForAccessibility="no"
            >overdue</Text>
          )}
        </View>
      </View>

      <Section label="Opened">
        <Text style={styles.value}>{new Date(d.openedAt).toLocaleString()}</Text>
      </Section>
      <Section label="SLA deadline">
        <Text style={[styles.value, d.overdue && styles.overdueValue]}>
          {new Date(d.dueAt).toLocaleString()}
        </Text>
      </Section>
      <Section label="Requested amount">
        <Text style={styles.value}>{amountStr}</Text>
      </Section>
      <Section label="Reason">
        <Text style={styles.value}>{d.reason}</Text>
      </Section>

      {d.hostResponse ? (
        <Section label="Host response">
          <Text style={styles.value}>{d.hostResponse}</Text>
        </Section>
      ) : null}

      {d.resolution ? (
        <Section label={d.status === 'rejected' ? 'Moderator decision' : 'Resolution'}>
          <Text style={styles.value}>{d.resolution}</Text>
        </Section>
      ) : null}

      <Text style={styles.sectionTitle}>Evidence</Text>
      {d.evidence && d.evidence.length > 0 ? (
        d.evidence.map((e) => (
          <View key={e.id} style={styles.evidenceItem}>
            {e.url ? <Text style={styles.evidenceUrl}>{e.url}</Text> : null}
            {e.note ? <Text style={styles.evidenceNote}>{e.note}</Text> : null}
            <Text style={styles.evidenceMeta}>{new Date(e.addedAt).toLocaleString()}</Text>
          </View>
        ))
      ) : (
        <Text style={styles.meta}>No evidence yet.</Text>
      )}

      {canAddEvidence && (
        <View style={styles.form}>
          <Text style={styles.formLabel} accessibilityRole="header">Add evidence</Text>
          <TextInput
            style={styles.input}
            placeholder="Link (e.g. photo URL)"
            placeholderTextColor="#999"
            autoCapitalize="none"
            value={evDraft.url}
            onChangeText={(v) => setEvDraft((s) => ({ ...s, url: v }))}
            accessibilityLabel="Evidence link"
            accessibilityHint="URL pointing to a photo or document supporting your case"
          />
          <TextInput
            style={[styles.input, { minHeight: 60 }]}
            multiline
            placeholder="Note"
            placeholderTextColor="#999"
            value={evDraft.note}
            onChangeText={(v) => setEvDraft((s) => ({ ...s, note: v }))}
            accessibilityLabel="Evidence note"
            accessibilityHint="Optional written context for the link, or just a note on its own"
          />
          <Pressable
            style={[styles.btn, busy && styles.btnDisabled]}
            disabled={busy}
            onPress={submitEvidence}
            accessibilityRole="button"
            accessibilityLabel={busy ? 'Saving evidence' : 'Add evidence'}
            accessibilityState={{ disabled: busy, busy }}
          >
            <Text style={styles.btnText}>{busy ? 'Saving…' : 'Add evidence'}</Text>
          </Pressable>
        </View>
      )}

      {canHostRespond && (
        <View style={styles.form}>
          <Text style={styles.formLabel} accessibilityRole="header">Post host response</Text>
          <TextInput
            style={[styles.input, { minHeight: 90 }]}
            multiline
            placeholder="Explain your side"
            placeholderTextColor="#999"
            value={hostDraft}
            onChangeText={setHostDraft}
            accessibilityLabel="Host response text"
            accessibilityHint="Your version of what happened; both the guest and the moderator will read this"
          />
          <Pressable
            style={[styles.btn, busy && styles.btnDisabled]}
            disabled={busy}
            onPress={submitHostResponse}
            accessibilityRole="button"
            accessibilityLabel={busy ? 'Saving response' : 'Post response'}
            accessibilityState={{ disabled: busy, busy }}
          >
            <Text style={styles.btnText}>{busy ? 'Saving…' : 'Post response'}</Text>
          </Pressable>
        </View>
      )}

      {/* S129: free-text reply composer. Parity with web S24's evidence form —
          guest OR host can post a body with optional photo URLs.
          TODO(media): inline picker — for now, URLs are paste-only to avoid
          pulling in expo-image-picker in this slice.
          TODO(i18n): all user-facing strings here are EN-only. */}
      {canReply && (
        <View style={styles.form} accessibilityLabel="Reply composer">
          <Text style={styles.formLabel} accessibilityRole="header">Reply</Text>
          <TextInput
            style={[styles.input, { minHeight: 90 }]}
            multiline
            maxLength={2000}
            placeholder="Add your reply to this case"
            placeholderTextColor="#999"
            value={replyDraft.body}
            onChangeText={(v) => setReplyDraft((s) => ({ ...s, body: v }))}
            accessibilityLabel="Reply body"
            accessibilityHint="Up to 2000 characters; visible to the other party and the moderator"
          />
          <TextInput
            style={styles.input}
            placeholder="Photo URLs, comma-separated (optional)"
            placeholderTextColor="#999"
            autoCapitalize="none"
            autoCorrect={false}
            value={replyDraft.urlsRaw}
            onChangeText={(v) => setReplyDraft((s) => ({ ...s, urlsRaw: v }))}
            accessibilityLabel="Photo evidence URLs"
            accessibilityHint="Paste one or more photo URLs separated by commas; leave empty to send a text-only reply"
          />
          <Pressable
            style={[styles.btn, (replyBusy || !replyDraft.body.trim()) && styles.btnDisabled]}
            disabled={replyBusy || !replyDraft.body.trim()}
            onPress={submitReply}
            accessibilityRole="button"
            accessibilityLabel={replyBusy ? 'Sending reply' : 'Send reply'}
            accessibilityState={{ disabled: replyBusy || !replyDraft.body.trim(), busy: replyBusy }}
          >
            <Text style={styles.btnText}>{replyBusy ? 'Sending…' : 'Send reply'}</Text>
          </Pressable>
        </View>
      )}

      <Pressable
        onPress={() => navigation.goBack()}
        style={styles.back}
        accessibilityRole="button"
        accessibilityLabel="Go back"
      >
        <Text style={styles.backText}>Back</Text>
      </Pressable>
    </ScrollView>
  );
}

function Section({ label, children }) {
  return (
    <View style={styles.section}>
      <Text style={styles.sectionLabel}>{label}</Text>
      {children}
    </View>
  );
}

const statusStyles = StyleSheet.create({
  open: { backgroundColor: '#fff4d6', color: '#8a5d00' },
  under_review: { backgroundColor: '#e6f0ff', color: '#1d4ed8' },
  resolved: { backgroundColor: '#dcfce7', color: '#166534' },
  rejected: { backgroundColor: '#fde2e2', color: '#991b1b' },
  overdue: { backgroundColor: '#fde2e2', color: '#991b1b' },
});

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff', padding: 12 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, backgroundColor: '#fff' },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 },
  title: { fontSize: 18, fontWeight: '800', color: '#222', textTransform: 'capitalize' },
  badges: { flexDirection: 'row', gap: 6 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 12, fontWeight: '700', overflow: 'hidden', fontSize: 12, textTransform: 'capitalize' },
  section: { paddingVertical: 6, borderBottomWidth: 1, borderColor: '#f0f0f0' },
  sectionLabel: { color: '#717171', fontSize: 12, fontWeight: '700', textTransform: 'uppercase' },
  value: { color: '#222', marginTop: 2 },
  overdueValue: { color: '#991b1b', fontWeight: '700' },
  sectionTitle: { fontSize: 16, fontWeight: '800', color: '#222', marginTop: 18, marginBottom: 8 },
  evidenceItem: { backgroundColor: '#fafafa', borderRadius: 8, padding: 10, marginBottom: 8 },
  evidenceUrl: { color: '#1d4ed8' },
  evidenceNote: { color: '#222', marginTop: 4 },
  evidenceMeta: { color: '#717171', fontSize: 12, marginTop: 6 },
  meta: { color: '#717171', marginTop: 4 },
  form: { backgroundColor: '#fafafa', borderRadius: 8, padding: 12, marginTop: 14 },
  formLabel: { fontWeight: '700', color: '#222', marginBottom: 6 },
  input: { borderWidth: 1, borderColor: '#ddd', borderRadius: 8, paddingHorizontal: 10, paddingVertical: 8, marginTop: 6, backgroundColor: '#fff' },
  btn: { backgroundColor: '#ff385c', borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10, alignSelf: 'flex-start', marginTop: 10 },
  btnDisabled: { opacity: 0.6 },
  btnText: { color: '#fff', fontWeight: '700' },
  back: { alignSelf: 'flex-start', marginTop: 20 },
  backText: { color: '#ff385c', fontWeight: '700' },
  error: { color: '#c0392b', marginBottom: 8 },
});
