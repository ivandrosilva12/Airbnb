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

export default function ConversationScreen({ route, navigation }) {
  const { id, title } = route.params;
  const api = useApi();
  const { subscribe } = useRealtime();
  const [messages, setMessages] = useState([]);
  const [myId, setMyId] = useState(null);
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState(null);
  const [blockedIds, setBlockedIds] = useState([]);

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
      const [me, res, blocks] = await Promise.all([
        api.me().catch(() => null),
        api.listMessages(id),
        api.listUserBlocks().catch(() => ({ blocked: [] })),
      ]);
      if (me) setMyId(me.id);
      setMessages(res.items || []);
      setBlockedIds(blocks?.blocked || []);
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
            <View style={[styles.bubble, mine ? styles.mine : styles.theirs]}>
              {!!item.body && <Text style={mine ? styles.mineText : styles.theirsText}>{item.body}</Text>}
              {att && isImage && (
                <Pressable onPress={() => Linking.openURL(att.url)}>
                  <Image source={{ uri: att.url }} style={styles.attachImage} resizeMode="cover" />
                </Pressable>
              )}
              {att && !isImage && (
                <Pressable onPress={() => Linking.openURL(att.url)}>
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
      {otherId && (
        <View style={styles.blockBar}>
          {isBlocked && <Text style={styles.blockNotice}>Blocked — messaging is disabled.</Text>}
          <Pressable onPress={toggleBlock}>
            <Text style={styles.blockLink}>{isBlocked ? 'Unblock' : 'Block user'}</Text>
          </Pressable>
        </View>
      )}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.quickReplies} contentContainerStyle={styles.quickRepliesContent}>
        {QUICK_REPLIES.map((q) => (
          <Pressable key={q} style={styles.quickChip} onPress={() => setDraft(q)}>
            <Text style={styles.quickChipText} numberOfLines={1}>{q}</Text>
          </Pressable>
        ))}
      </ScrollView>
      <View style={styles.composer}>
        <Pressable style={styles.attachBtn} onPress={attach} disabled={sending}>
          <Text style={styles.attachText}>📎</Text>
        </Pressable>
        <TextInput
          style={styles.input}
          placeholder="Message…"
          value={draft}
          onChangeText={setDraft}
          multiline
        />
        <Pressable style={[styles.sendBtn, sending && styles.sendBtnDisabled]} onPress={send} disabled={sending}>
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
  blockBar: { flexDirection: 'row', alignItems: 'center', justifyContent: 'flex-end', gap: 10, paddingHorizontal: 12, paddingVertical: 6, borderTopWidth: 1, borderColor: '#eee' },
  blockNotice: { color: '#c0392b', fontSize: 12, marginRight: 'auto' },
  blockLink: { color: '#c0392b', fontWeight: '600', fontSize: 13 },
  quickReplies: { maxHeight: 44, borderTopWidth: 1, borderColor: '#eee' },
  quickRepliesContent: { gap: 6, paddingHorizontal: 10, paddingVertical: 6, alignItems: 'center' },
  quickChip: { backgroundColor: '#f7f7f7', borderWidth: 1, borderColor: '#ddd', borderRadius: 999, paddingHorizontal: 11, paddingVertical: 5, maxWidth: 220 },
  quickChipText: { color: '#222', fontSize: 12 },
  composer: { flexDirection: 'row', alignItems: 'flex-end', gap: 8, padding: 10, borderTopWidth: 1, borderColor: '#eee' },
  attachBtn: { paddingHorizontal: 8, paddingVertical: 8, justifyContent: 'center' },
  attachText: { fontSize: 22 },
  input: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 18, paddingHorizontal: 14, paddingVertical: 8, maxHeight: 100 },
  sendBtn: { backgroundColor: '#ff385c', borderRadius: 18, paddingHorizontal: 18, paddingVertical: 10 },
  sendBtnDisabled: { opacity: 0.6 },
  sendText: { color: '#fff', fontWeight: '700' },
});
