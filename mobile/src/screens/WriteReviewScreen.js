import { useState } from 'react';
import { ScrollView, View, Text, TextInput, Pressable, StyleSheet, ActivityIndicator } from 'react-native';
import { useApi } from '../api/useApi';
import { useAuth } from '../auth/AuthContext';
import { useT } from '../i18n/I18nContext';

// WriteReviewScreen is the mobile counterpart of the web's inline ReviewForm
// in MyTrips.jsx (S80 post-stay review flow, extended by S114 multi-criteria
// reviews). The web build keeps the form inline next to each row because the
// trips table has the horizontal room; on mobile we promote the form to a
// dedicated screen so the keyboard, the six category pickers and the multi-
// line comment box all have room to breathe.
//
// Route params: { bookingId, propertyTitle, hostName } — the listing title
// and host name are rendered as a header so the user has visual confirmation
// of which stay they're rating before they tap submit. propertyTitle and
// hostName are optional; we degrade gracefully when only bookingId is set
// (e.g. when the screen is navigated to via deep-link without ancillary
// context).
//
// REVIEW_CATEGORIES matches the web (web/src/pages/MyTrips.jsx) and the
// existing mobile inline form on TripsScreen.js exactly. The dotted-key
// namespace `review.cat.*` is shared across web and mobile translations.
const REVIEW_CATEGORIES = ['cleanliness', 'accuracy', 'communication', 'location', 'checkIn', 'value'];

// MAX_COMMENT mirrors the dispute reply composer (DisputeScreen.js) and the
// web dispute reason limit — 2000 chars is plenty of room for a thoughtful
// stay review without becoming a free-form attack surface.
const MAX_COMMENT = 2000;

export default function WriteReviewScreen({ route, navigation }) {
  const api = useApi();
  const { authenticated, login } = useAuth();
  const { t } = useT();

  const bookingId = route?.params?.bookingId;
  const propertyTitle = route?.params?.propertyTitle || '';
  const hostName = route?.params?.hostName || '';

  // Overall rating defaults to 5 to match the web's <select> default. The
  // server requires rating ∈ [1,5]; we never let the user submit below 1.
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  // cats is a partial map keyed by category. Categories left at 0 / missing
  // are NOT sent to the server — the wire contract treats `categories` as
  // optional and an "any positive" check decides whether to include it.
  const [cats, setCats] = useState({});
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  // success is shown briefly before we navigate back so the user gets a
  // confirmation cue inside this screen rather than relying on the
  // destination to surface it. The actual reviewed-state is rehydrated by
  // TripsScreen on focus via its pendingReviews() call.
  const [success, setSuccess] = useState(false);

  const canSubmit = !submitting && rating >= 1 && comment.trim().length > 0;

  async function submit() {
    if (!canSubmit) return;
    setError(null);
    setSubmitting(true);
    try {
      const categories = {};
      let any = false;
      for (const k of REVIEW_CATEGORIES) {
        const v = Number(cats[k] || 0);
        categories[k] = v;
        if (v > 0) any = true;
      }
      await api.createReview({
        bookingId,
        rating: Number(rating),
        comment: comment.trim(),
        // Only attach `categories` when the guest actually filled at least
        // one sub-score — matches the web contract and avoids storing a
        // row of zeros for guests who only rate overall.
        categories: any ? categories : undefined,
      });
      setSuccess(true);
      // Short pause so the success banner is visible before we pop back.
      // 700 ms is short enough to feel responsive but long enough to read.
      setTimeout(() => {
        if (navigation?.canGoBack?.()) navigation.goBack();
      }, 700);
    } catch (e) {
      setError(e.message);
    } finally {
      setSubmitting(false);
    }
  }

  if (!authenticated) {
    return (
      <View style={styles.center}>
        {/* TODO(i18n): detail.signInReserve is the closest existing key; a
            dedicated review.signInHint would be more accurate. */}
        <Text style={styles.meta}>{t('detail.signInReserve')}</Text>
        <Pressable style={styles.btn} onPress={login}>
          <Text style={styles.btnText}>{t('nav.signIn')}</Text>
        </Pressable>
      </View>
    );
  }

  // Guard against a missing booking id — usually means a stale navigation
  // entry. We render a friendly empty state rather than letting createReview
  // fail with an opaque 400.
  if (!bookingId) {
    return (
      <View style={styles.center}>
        {/* TODO(i18n): review.missingBooking */}
        <Text style={styles.meta}>No booking selected.</Text>
      </View>
    );
  }

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={styles.content}
      keyboardShouldPersistTaps="handled"
    >
      {/* Header — anchors the review to a specific stay so the user can
          double-check before they submit. Both fields are optional so we
          only render the lines that have content. */}
      {(propertyTitle || hostName) && (
        <View style={styles.header}>
          {propertyTitle ? <Text style={styles.title}>{propertyTitle}</Text> : null}
          {hostName ? (
            // TODO(i18n): review.hostedBy
            <Text style={styles.meta}>Hosted by {hostName}</Text>
          ) : null}
        </View>
      )}

      {success && (
        // TODO(i18n): review.submittedSuccess
        <View style={styles.successBanner} accessibilityLiveRegion="polite">
          <Text style={styles.successText}>Review submitted. Thanks!</Text>
        </View>
      )}

      {error && (
        <Text style={styles.error} accessibilityLiveRegion="polite">
          {error}
        </Text>
      )}

      {/* Overall rating — five-star picker. Matches the inline form's star
          pattern from TripsScreen.js exactly so users moving between the
          two entry points see the same affordance. */}
      <Text style={styles.label}>{t('review.overall')}</Text>
      <View style={styles.starRow} accessibilityRole="adjustable" accessibilityLabel={t('review.overall')}>
        {[1, 2, 3, 4, 5].map((n) => (
          <Pressable
            key={n}
            onPress={() => setRating(n)}
            accessibilityRole="button"
            // TODO(i18n): review.starN
            accessibilityLabel={`${n} stars`}
            accessibilityState={{ selected: n === rating }}
          >
            <Text style={n <= rating ? styles.starOn : styles.starOff}>★</Text>
          </Pressable>
        ))}
      </View>

      {/* Sub-scores — optional. Web parity (S114): cleanliness, accuracy,
          communication, location, check-in, value. Each is an independent
          1-5 picker; an unset row submits as 0 and is dropped before the
          API call. */}
      {/* TODO(i18n): review.subTitle */}
      <Text style={styles.subTitle}>How was the stay in detail? (optional)</Text>
      {REVIEW_CATEGORIES.map((k) => (
        <View key={k} style={styles.catRow}>
          <Text style={styles.catLabel}>{t(`review.cat.${k}`)}</Text>
          <View style={styles.starRow}>
            {[1, 2, 3, 4, 5].map((n) => (
              <Pressable
                key={n}
                onPress={() => setCats((c) => ({ ...c, [k]: n }))}
                accessibilityRole="button"
                accessibilityLabel={`${t(`review.cat.${k}`)}: ${n} stars`}
                accessibilityState={{ selected: n === (cats[k] || 0) }}
              >
                <Text style={n <= (cats[k] || 0) ? styles.starOnSm : styles.starOffSm}>★</Text>
              </Pressable>
            ))}
          </View>
        </View>
      ))}

      {/* Comment — multiline, capped at MAX_COMMENT (2000). The counter
          shows how much room is left; the submit button is disabled until
          there's at least one non-whitespace character. */}
      <Text style={styles.label}>{t('trips.shareExperience')}</Text>
      <TextInput
        style={styles.commentInput}
        multiline
        maxLength={MAX_COMMENT}
        // TODO(i18n): review.placeholder
        placeholder="Tell future guests what stood out about this stay."
        placeholderTextColor="#999"
        value={comment}
        onChangeText={setComment}
        accessibilityLabel={t('trips.shareExperience')}
      />
      <Text style={styles.counter}>
        {comment.length}/{MAX_COMMENT}
      </Text>

      <View style={styles.actions}>
        <Pressable
          style={[styles.btn, !canSubmit && styles.btnDisabled]}
          disabled={!canSubmit}
          onPress={submit}
          accessibilityRole="button"
          accessibilityState={{ disabled: !canSubmit, busy: submitting }}
          // TODO(i18n): review.submitA11y
          accessibilityLabel="Submit review"
        >
          {submitting ? (
            <ActivityIndicator color="#fff" />
          ) : (
            <Text style={styles.btnText}>{t('common.submit')}</Text>
          )}
        </Pressable>
        <Pressable
          onPress={() => navigation?.goBack?.()}
          accessibilityRole="button"
          accessibilityLabel={t('common.cancel')}
        >
          <Text style={styles.cancel}>{t('common.cancel')}</Text>
        </Pressable>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  content: { padding: 16, paddingBottom: 40 },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    backgroundColor: '#fff',
    padding: 24,
  },
  header: { marginBottom: 16 },
  title: { fontSize: 18, fontWeight: '800', color: '#222' },
  meta: { color: '#717171', marginTop: 4 },
  successBanner: {
    backgroundColor: '#e8f6ee',
    borderColor: '#a3d9b4',
    borderWidth: 1,
    borderRadius: 8,
    padding: 10,
    marginBottom: 12,
  },
  successText: { color: '#1e7e44', fontWeight: '700' },
  error: { color: '#c0392b', marginBottom: 8 },
  label: { fontWeight: '700', color: '#222', marginTop: 12, marginBottom: 6 },
  subTitle: { fontWeight: '700', color: '#222', marginTop: 18, marginBottom: 4 },
  starRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  starOn: { color: '#ff385c', fontSize: 30 },
  starOff: { color: '#ddd', fontSize: 30 },
  starOnSm: { color: '#ff385c', fontSize: 20 },
  starOffSm: { color: '#ddd', fontSize: 20 },
  catRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: 8,
    borderBottomWidth: 1,
    borderColor: '#f2f2f2',
  },
  catLabel: { color: '#444', fontSize: 15 },
  commentInput: {
    borderWidth: 1,
    borderColor: '#ddd',
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    minHeight: 120,
    textAlignVertical: 'top',
    color: '#222',
  },
  counter: { color: '#717171', fontSize: 12, marginTop: 4, textAlign: 'right' },
  actions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 18,
    marginTop: 20,
  },
  btn: {
    backgroundColor: '#ff385c',
    borderRadius: 8,
    paddingHorizontal: 22,
    paddingVertical: 12,
    minWidth: 120,
    alignItems: 'center',
  },
  btnDisabled: { backgroundColor: '#f6a6b8' },
  btnText: { color: '#fff', fontWeight: '700' },
  cancel: { color: '#717171', fontWeight: '600' },
});
