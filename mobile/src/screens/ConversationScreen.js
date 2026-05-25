import { useEffect, useRef, useState, useCallback, useLayoutEffect } from 'react';
import { View, Text, FlatList, TextInput, Pressable, StyleSheet, ActivityIndicator, KeyboardAvoidingView, Platform, Image, Linking } from 'react-native';
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
  // Keep the latest api in a ref so the realtime subscription (registered once)
  // always uses a live token without re-subscribing on every render.
  const apiRef = useRef(api);
  apiRef.current = api;

  useLayoutEffect(() => {
    if (title) navigation.setOptions({ title });
  }, [navigation, title]);

  const load = useCallback(async () => {
    try {
      const [me, res] = await Promise.all([api.me().catch(() => null), api.listMessages(id)]);
      if (me) setMyId(me.id);
      setMessages(res.items || []);
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
      <View style={styles.composer}>
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
  composer: { flexDirection: 'row', alignItems: 'flex-end', gap: 8, padding: 10, borderTopWidth: 1, borderColor: '#eee' },
  input: { flex: 1, borderWidth: 1, borderColor: '#ddd', borderRadius: 18, paddingHorizontal: 14, paddingVertical: 8, maxHeight: 100 },
  sendBtn: { backgroundColor: '#ff385c', borderRadius: 18, paddingHorizontal: 18, paddingVertical: 10 },
  sendBtnDisabled: { opacity: 0.6 },
  sendText: { color: '#fff', fontWeight: '700' },
});
