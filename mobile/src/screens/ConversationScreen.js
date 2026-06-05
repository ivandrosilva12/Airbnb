import { useEffect, useRef, useState, useCallback, useLayoutEffect } from 'react';
import { View, Text, FlatList, TextInput, Pressable, StyleSheet, ActivityIndicator, KeyboardAvoidingView, Platform, Image, Linking, ScrollView } from 'react-native';

// Canned quick-reply templates a user can tap to fill the composer.
const QUICK_REPLIES = [
  'Hi! Thanks for reaching out 😊',
  'Yes, those dates are available.',
  'Sorry, those dates are not available.',
  'Check-in is from 3 PM and check-out by 11 AM.',
  'Let me check and get back to you shortly.',
  'Thank you — looking forward to hosting you!',
];
import * as ImagePicker from 'expo-image-picker';
import { useApi } from '../api/useApi';
import { useRealtime } from '../api/RealtimeContext';

// TODO(i18n): translate — wrap hardcoded labels via useT() from ../i18n/I18nContext.
// Key namespace mirrors the web (msg.*, msg.qr.*).

export default function ConversationScreen({ route, navigation }) {
  const { id, title, isTeamMailbox } = route.params;
  const api = useApi();
  const { subscribe } = useRealtime();
  const [messages, setMessages] = useState([]);
  const [myId, setMyId] = useState(null);
  const [myRole, setMyRole] = useState(null);
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState(null);
  const [blockedIds, setBlockedIds] = useState([]);
  // savedTemplates is the host's per-user playbook (S20b). Empty for users
  // who never bother creating one — the chip row just stops at the canned
  // QUICK_REPLIES in that case, no extra section, no visual clutter.
  const [savedTemplates, setSavedTemplates] = useState([]);
  // S121: inline saved-replies picker. The chip row above the composer is
  // a peek; this panel is the full list (label + body preview) and is the
  // hosts-only workflow for hosts with a deep playbook. The picker is
  // hidden behind a toggle so it doesn't crowd the chat by default.
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerLoading, setPickerLoading] = useState(false);
  // selection tracks the TextInput caret so a tapped template inserts at
  // the cursor instead of being appended at the end. Fallback is append.
  const [selection, setSelection] = useState({ start: 0, end: 0 });
  const isHost = myRole === 'host' || myRole === 'admin';

  // The counterparty is the sender of any message that isn't ours.
  const otherId = messages.find((m) => myId && m.senderId !== myId)?.senderId || null;
  const isBlocked = otherId ? blockedIds.includes(otherId) : false;

  async function toggleBlock() {
    if (!otherId) return;
    setError(null);
    try {
      if (isBlocked) {
        await api.unblockUser(otherId);
        setBlockedIds((b) => b.filter((x) => x !== otherId));
      } else {
        await api.blockUser(otherId);
        setBlockedIds((b) => [...b, otherId]);
      }
    } catch (e) {
      setError(e.message);
    }
  }
  // Keep the latest api in a ref so the realtime subscription (registered once)
  // always uses a live token without re-subscribing on every render.
  const apiRef = useRef(api);
  apiRef.current = api;

  useLayoutEffect(() => {
    if (title) navigation.setOptions({ title });
  }, [navigation, title]);

  const load = useCallback(async () => {
    try {
      const [me, res, blocks, tpls] = await Promise.all([
        api.me().catch(() => null),
        api.listMessages(id),
        api.listUserBlocks().catch(() => ({ blocked: [] })),
        // Silent failure: a guest with no saved templates just gets [].
        api.listMessageTemplates().catch(() => ({ items: [] })),
      ]);
      if (me) {
        setMyId(me.id);
        setMyRole(me.role || null);
      }
      setMessages(res.items || []);
      setBlockedIds(blocks?.blocked || []);
      setSavedTemplates(tpls?.items || []);
      api.markConversationRead(id).catch(() => {});
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [api, id]);

  useEffect(() => {
    load();
  }, [id]);

  // Live updates: append inbound messages the moment the server pushes a
  // "message" event for this conversation (no polling).
  useEffect(
    () =>
      subscribe((update) => {
        if (update.type !== 'message' || update.conversationId !== id) return;
        apiRef.current
          .listMessages(id)
          .then((res) => setMessages(res.items || []))
          .catch(() => {});
        apiRef.current.markConversationRead(id).catch(() => {});
      }),
    [subscribe, id],
  );

  async function send() {
    const body = draft.trim();
    if (!body) return;
    setSending(true);
    setError(null);
    try {
      await api.sendMessage(id, body);
      setDraft('');
      const res = await api.listMessages(id);
      setMessages(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setSending(false);
    }
  }

  async function attach() {
    setError(null);
    const perm = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (!perm.granted) {
      setError('Photo library permission is required.');
      return;
    }
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ImagePicker.MediaTypeOptions.Images, quality: 0.7 });
    if (res.canceled || !res.assets?.length) return;
    const asset = res.assets[0];
    setSending(true);
    try {
      await api.sendAttachment(id, {
        uri: asset.uri,
        name: asset.fileName || 'photo.jpg',
        type: asset.mimeType || 'image/jpeg',
      });
      const list = await api.listMessages(id);
      setMessages(list.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setSending(false);
    }
  }

  // togglePicker opens/closes the saved-replies panel. We re-fetch on open
  // so a host who just added a template in another tab sees it instantly,
  // and a host whose only template just disappeared gets the empty state.
  async function togglePicker() {
    if (pickerOpen) {
      setPickerOpen(false);
      return;
    }
    setPickerOpen(true);
    setPickerLoading(true);
    try {
      const tpls = await api.listMessageTemplates();
      setSavedTemplates(tpls?.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setPickerLoading(false);
    }
  }

  // insertTemplate splices the body into the draft at the current cursor
  // position. If the caret is unknown (never focused) we append. The picker
  // closes after a pick because a host typically inserts one template at a
  // time — re-opening is cheap and matches the web's modal-like feel.
  function insertTemplate(body) {
    setDraft((prev) => {
      const start = Math.min(selection.start ?? prev.length, prev.length);
      const end = Math.min(selection.end ?? prev.length, prev.length);
      if (start === 0 && end === 0 && prev.length > 0) {
        // No real cursor capture yet — append with a separator if there's
        // already text so we don't run words together.
        const sep = /\s$/.test(prev) ? '' : ' ';
        return prev + sep + body;
      }
      return prev.slice(0, start) + body + prev.slice(end);
    });
    setPickerOpen(false);
  }

  if (loading) return <ActivityIndicator style={{ flex: 1 }} color="#ff385c" />;

  return (
    <KeyboardAvoidingView style={styles.container} behavior={Platform.OS === 'ios' ? 'padding' : undefined} keyboardVerticalOffset={90}>
      <FlatList
        style={{ flex: 1 }}
        contentContainerStyle={{ padding: 12 }}
        data={messages}
        keyExtractor={(m) => m.id}
        ListEmptyComponent={<Text style={styles.empty}>No messages yet. Say hello!</Text>}
        renderItem={({ item }) => {
          const mine = myId && item.senderId === myId;
          const att = item.attachment;
          const isImage = att && att.contentType && att.contentType.startsWith('image/');
          return (
            <View
              style={[styles.bubble, mine ? styles.mine : styles.theirs]}
              accessibilityLabel={`${mine ? 'You' : 'They'} said: ${item.body || (att ? `attachment ${att.filename || ''}` : '')}`}
            >
              {!!item.body && <Text style={mine ? styles.mineText : styles.theirsText}>{item.body}</Text>}
              {att && isImage && (
                <Pressable
                  onPress={() => Linking.openURL(att.url)}
                  accessibilityRole="link"
                  accessibilityLabel={`Open photo ${att.filename || 'attachment'}`}
                >
                  <Image
                    source={{ uri: att.url }}
                    style={styles.attachImage}
                    resizeMode="cover"
                    accessible={false}
                  />
                </Pressable>
              )}
              {att && !isImage && (
                <Pressable
                  onPress={() => Linking.openURL(att.url)}
                  accessibilityRole="link"
                  accessibilityLabel={`Open file ${att.filename || 'attachment'}`}
                >
                  <Text style={[mine ? styles.mineText : styles.theirsText, styles.attachLink]}>
                    📎 {att.filename || 'Attachment'}
                  </Text>
                </Pressable>
              )}
            </View>
          );
        }}
      />
      {error && <Text style={styles.error}>{error}</Text>}
      {isTeamMailbox && (
        <Text style={styles.teamBanner}>
          Replying on behalf of the host — the guest sees the host's name, not yours.
        </Text>
      )}
      {/* In team-mailbox mode the cohost is a third party helping the host —
          they should not be able to block the guest or the host they're
          assisting, so we hide the block bar entirely. */}
      {otherId && !isTeamMailbox && (
        <View style={styles.blockBar}>
          {isBlocked && <Text style={styles.blockNotice} accessibilityRole="alert">Blocked — messaging is disabled.</Text>}
          <Pressable
            onPress={toggleBlock}
            accessibilityRole="button"
            accessibilityLabel={isBlocked ? 'Unblock this user' : 'Block this user'}
            accessibilityHint={isBlocked ? 'Allows them to message you again' : 'Stops messages in both directions'}
          >
            <Text style={styles.blockLink}>{isBlocked ? 'Unblock' : 'Block user'}</Text>
          </Pressable>
        </View>
      )}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.quickReplies}
        contentContainerStyle={styles.quickRepliesContent}
        accessibilityLabel="Quick replies"
      >
        {QUICK_REPLIES.map((q) => (
          <Pressable
            key={q}
            style={styles.quickChip}
            onPress={() => setDraft(q)}
            accessibilityRole="button"
            accessibilityLabel={`Insert quick reply: ${q}`}
          >
            <Text style={styles.quickChipText} numberOfLines={1}>{q}</Text>
          </Pressable>
        ))}
        {/* The host's saved replies — chip uses the label, taps insert the
            body. Distinct rose-tinted style so users see "this is mine"
            vs the platform's canned set. */}
        {savedTemplates.map((tpl) => (
          <Pressable
            key={tpl.id}
            style={styles.savedChip}
            onPress={() => setDraft(tpl.body)}
            accessibilityRole="button"
            accessibilityLabel={`Insert saved reply: ${tpl.label}`}
            accessibilityHint={tpl.body.length > 80 ? `${tpl.body.slice(0, 80)}…` : tpl.body}
          >
            <Text style={styles.savedChipText} numberOfLines={1}>{tpl.label}</Text>
          </Pressable>
        ))}
      </ScrollView>
      {/* S121: hosts-only inline saved-replies picker. Rendered above the
          composer so the panel pushes the input down (not the chat up) and
          stays out of the keyboard's way. Closed by default. */}
      {isHost && pickerOpen && (
        <View style={styles.pickerPanel} accessibilityLabel="Saved replies picker">
          <View style={styles.pickerHeader}>
            {/* TODO(i18n) */}
            <Text style={styles.pickerTitle}>Saved replies</Text>
            <Pressable
              onPress={togglePicker}
              accessibilityRole="button"
              accessibilityLabel="Close saved replies picker"
            >
              <Text style={styles.pickerClose}>Close</Text>
            </Pressable>
          </View>
          {pickerLoading ? (
            <ActivityIndicator style={{ marginVertical: 12 }} color="#ff385c" />
          ) : savedTemplates.length === 0 ? (
            // TODO(i18n)
            <Text style={styles.pickerEmpty}>
              No saved replies yet. Tap your Account → Saved replies to create one.
            </Text>
          ) : (
            <ScrollView style={styles.pickerList} keyboardShouldPersistTaps="handled">
              {savedTemplates.map((tpl) => {
                const heading = (tpl.label && tpl.label.trim()) || tpl.body.split('\n')[0];
                return (
                  <Pressable
                    key={tpl.id}
                    style={styles.pickerRow}
                    onPress={() => insertTemplate(tpl.body)}
                    accessibilityRole="button"
                    accessibilityLabel={`Insert saved reply: ${heading}`}
                  >
                    <Text style={styles.pickerRowTitle} numberOfLines={1}>{heading}</Text>
                    <Text style={styles.pickerRowBody} numberOfLines={2}>{tpl.body}</Text>
                  </Pressable>
                );
              })}
            </ScrollView>
          )}
        </View>
      )}
      <View style={styles.composer}>
        <Pressable
          style={styles.attachBtn}
          onPress={attach}
          disabled={sending}
          accessibilityRole="button"
          accessibilityLabel="Attach a photo"
          accessibilityHint="Opens the photo library to pick an image to send"
          accessibilityState={{ disabled: sending }}
        >
          <Text style={styles.attachText}>📎</Text>
        </Pressable>
        <TextInput
          style={styles.input}
          placeholder="Message…"
          value={draft}
          onChangeText={setDraft}
          onSelectionChange={(e) => setSelection(e.nativeEvent.selection)}
          multiline
          accessibilityLabel="Message text"
        />
        {/* Hosts-only templates toggle. Sits between input and Send so it's
            thumb-reachable and visually paired with the send action. */}
        {isHost && (
          <Pressable
            style={[styles.templatesBtn, pickerOpen && styles.templatesBtnActive]}
            onPress={togglePicker}
            accessibilityRole="button"
            accessibilityLabel={pickerOpen ? 'Close saved replies' : 'Open saved replies'}
            accessibilityState={{ expanded: pickerOpen }}
          >
            <Text style={styles.templatesText}>📋</Text>
          </Pressable>
        )}
        <Pressable
          style={[styles.sendBtn, sending && styles.sendBtnDisabled]}
          onPress={send}
          disabled={sending}
          accessibilityRole="button"
          accessibilityLabel={sending ? 'Sending message' : 'Send message'}
          accessibilityState={{ disabled: sending, busy: sending }}
        >
          <Text style={styles.sendText}>Send</Text>
        </Pressable>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  bubble: { maxWidth: '80%', borderRadius: 14, paddingHorizontal: 12, paddingVertical: 8, marginVertical: 4 },
  mine: { alignSelf: 'flex-end', backgroundColor: '#ff385c' },
  theirs: { alignSelf: 'flex-start', backgroundColor: '#f0f0f0' },
  mineText: { color: '#fff' },
  theirsText: { color: '#222' },
  attachImage: { width: 200, height: 200, borderRadius: 8, marginTop: 6 },
  attachLink: { marginTop: 6, textDecorationLine: 'underline' },
  empty: { textAlign: 'center', color: '#717171', marginTop: 24 },
  error: { color: '#c0392b', paddingHorizontal: 12 },
  teamBanner: { backgroundColor: '#e6f0ff', color: '#1d4ed8', paddingHorizontal: 12, paddingVertical: 8, borderTopWidth: 1, borderColor: '#cfe0ff', fontWeight: '600', fontSize: 12 },
  blockBar: { flexDirection: 'row', alignItems: 'center', justifyContent: 'flex-end', gap: 10, paddingHorizontal: 12, paddingVertical: 6, borderTopWidth: 1, borderColor: '#eee' },
  blockNotice: { color: '#c0392b', fontSize: 12, marginRight: 'auto' },
  blockLink: { color: '#c0392b', fontWeight: '600', fontSize: 13 },
  quickReplies: { maxHeight: 44, borderTopWidth: 1, borderColor: '#eee' },
  quickRepliesContent: { gap: 6, paddingHorizontal: 10, paddingVertical: 6, alignItems: 'center' },
  quickChip: { backgroundColor: '#f7f7f7', borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 11, paddingVertical: 5, maxWidth: 220 },
  quickChipText: { color: '#222', fontSize: 12 },
  savedChip: { backgroundColor: '#fff0f3', borderWidth: 1, borderColor: '#ffc9d4', borderRadius: 999, paddingHorizontal: 11, paddingVertical: 5, maxWidth: 220 },
  savedChipText: { color: '#a02043', fontSize: 12, fontWeight: '600' },
  composer: { flexDirection: 'row', alignItems: 'flex-end', gap: 8, padding: 10, borderTopWidth: 1, borderColor: '#eee' },
  attachBtn: { paddingHorizontal: 8, paddingVertical: 8, justifyContent: 'center' },
  attachText: { fontSize: 22 },
  input: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 18, paddingHorizontal: 14, paddingVertical: 8, maxHeight: 100 },
  templatesBtn: { paddingHorizontal: 8, paddingVertical: 8, justifyContent: 'center', borderRadius: 8 },
  templatesBtnActive: { backgroundColor: '#fff0f3' },
  templatesText: { fontSize: 20 },
  sendBtn: { backgroundColor: '#ff385c', borderRadius: 18, paddingHorizontal: 18, paddingVertical: 10 },
  sendBtnDisabled: { opacity: 0.6 },
  sendText: { color: '#fff', fontWeight: '700' },
  pickerPanel: { maxHeight: 260, borderTopWidth: 1, borderColor: '#eee', backgroundColor: '#fafafa' },
  pickerHeader: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 12, paddingVertical: 8, borderBottomWidth: 1, borderColor: '#eee' },
  pickerTitle: { fontWeight: '700', color: '#222' },
  pickerClose: { color: '#ff385c', fontWeight: '600' },
  pickerEmpty: { color: '#717171', textAlign: 'center', paddingHorizontal: 16, paddingVertical: 18 },
  pickerList: { paddingHorizontal: 8, paddingVertical: 4 },
  pickerRow: { paddingHorizontal: 8, paddingVertical: 10, borderBottomWidth: 1, borderColor: '#eee' },
  pickerRowTitle: { color: '#222', fontWeight: '700', fontSize: 14 },
  pickerRowBody: { color: '#717171', fontSize: 12, marginTop: 2 },
});
