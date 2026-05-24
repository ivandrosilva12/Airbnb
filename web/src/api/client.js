import keycloak from '../keycloak';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8081/api/v1';

// request is a thin fetch wrapper that attaches the bearer token (refreshing it
// when close to expiry) and normalises error handling.
async function request(method, path, { body, formData, auth = false } = {}) {
  const headers = {};
  const opts = { method, headers };

  if (auth && keycloak.authenticated) {
    try {
      await keycloak.updateToken(30);
    } catch {
      keycloak.login();
      return;
    }
    headers.Authorization = `Bearer ${keycloak.token}`;
  }

  if (formData) {
    opts.body = formData;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(`${BASE_URL}${path}`, opts);
  if (res.status === 204) return null;

  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const message = (data && data.error) || res.statusText;
    throw new Error(message);
  }
  return data;
}

// downloadFile fetches an authenticated binary endpoint and triggers a browser
// download, since a plain link cannot carry the bearer token.
async function downloadFile(path, filename) {
  if (!keycloak.authenticated) {
    keycloak.login();
    return;
  }
  try {
    await keycloak.updateToken(30);
  } catch {
    keycloak.login();
    return;
  }
  const res = await fetch(`${BASE_URL}${path}`, {
    headers: { Authorization: `Bearer ${keycloak.token}` },
  });
  if (!res.ok) throw new Error(`Download failed (${res.status})`);
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// buildQuery turns an object into a query string, repeating the key for array
// values (e.g. { amenity: ['wifi','tv'] } -> "amenity=wifi&amenity=tv").
function buildQuery(params) {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) v.forEach((item) => qs.append(k, item));
    else qs.append(k, v);
  }
  return qs.toString();
}

export const api = {
  // Listings (public)
  searchProperties: (params = {}) => {
    const qs = buildQuery(params);
    return request('GET', `/properties${qs ? `?${qs}` : ''}`);
  },
  getProperty: (id) => request('GET', `/properties/${id}`),
  listAmenities: () => request('GET', '/amenities'),
  getReviews: (id) => request('GET', `/properties/${id}/reviews`),
  getReviewSummary: (id) => request('GET', `/properties/${id}/reviews/summary`),

  // Profile
  me: () => request('GET', '/me', { auth: true }),
  updatePreferences: (prefs) => request('PATCH', '/me/preferences', { body: prefs, auth: true }),
  becomeHost: () => request('POST', '/me/become-host', { auth: true }),

  // Identity verification (KYC)
  getVerification: () => request('GET', '/me/verification', { auth: true }),
  submitVerification: (body) => request('POST', '/me/verification', { body, auth: true }),

  // Listing reports
  reportListing: (propertyId, body) => request('POST', `/properties/${propertyId}/reports`, { body, auth: true }),

  // Admin moderation
  adminListVerifications: () => request('GET', '/admin/verifications', { auth: true }),
  adminApproveVerification: (id) => request('POST', `/admin/verifications/${id}/approve`, { auth: true }),
  adminRejectVerification: (id, reason) => request('POST', `/admin/verifications/${id}/reject`, { body: { reason }, auth: true }),
  adminListReports: () => request('GET', '/admin/reports', { auth: true }),
  adminResolveReport: (id, resolution) => request('POST', `/admin/reports/${id}/resolve`, { body: { resolution }, auth: true }),
  adminDismissReport: (id, resolution) => request('POST', `/admin/reports/${id}/dismiss`, { body: { resolution }, auth: true }),
  adminSuspendProperty: (id) => request('POST', `/admin/properties/${id}/suspend`, { auth: true }),
  adminUnsuspendProperty: (id) => request('POST', `/admin/properties/${id}/unsuspend`, { auth: true }),

  // Live alert state (firing + recently resolved), pushed by Alertmanager
  adminListAlerts: () => request('GET', '/admin/alerts', { auth: true }),

  // Alertmanager silences (maintenance windows that mute alerts)
  adminListSilences: () => request('GET', '/admin/alerts/silences', { auth: true }),
  adminCreateSilence: (body) => request('POST', '/admin/alerts/silences', { body, auth: true }),
  adminDeleteSilence: (id) => request('DELETE', `/admin/alerts/silences/${id}`, { auth: true }),

  // Bookings
  createBooking: (body) => request('POST', '/bookings', { body, auth: true }),
  myBookings: () => request('GET', '/bookings/me', { auth: true }),
  cancelBooking: (id) => request('POST', `/bookings/${id}/cancel`, { auth: true }),

  // Reviews
  createReview: (body) => request('POST', '/reviews', { body, auth: true }),
  createGuestReview: (body) => request('POST', '/reviews/guest', { body, auth: true }),
  myGuestReviews: () => request('GET', '/me/guest-reviews', { auth: true }),
  myPendingReviews: () => request('GET', '/me/reviews/pending', { auth: true }),

  // Host
  hostMetrics: () => request('GET', '/host/metrics', { auth: true }),
  hostEarnings: () => request('GET', '/host/earnings', { auth: true }),
  hostEarningEntries: () => request('GET', '/host/earnings/entries', { auth: true }),
  downloadEarningsCsv: () => downloadFile('/host/earnings/export.csv', 'airhost-earnings.csv'),
  myProperties: () => request('GET', '/host/properties', { auth: true }),
  createProperty: (body) => request('POST', '/properties', { body, auth: true }),
  publishProperty: (id) => request('POST', `/properties/${id}/publish`, { auth: true }),
  deleteProperty: (id) => request('DELETE', `/properties/${id}`, { auth: true }),
  uploadPhoto: (id, file) => {
    const fd = new FormData();
    fd.append('photo', file);
    return request('POST', `/properties/${id}/photos`, { formData: fd, auth: true });
  },
  reorderPhotos: (id, photoIds) => request('PATCH', `/properties/${id}/photos/order`, { body: { photoIds }, auth: true }),
  deletePhoto: (id, photoId) => request('DELETE', `/properties/${id}/photos/${photoId}`, { auth: true }),
  propertyBookings: (id) => request('GET', `/properties/${id}/bookings`, { auth: true }),
  confirmBooking: (id) => request('POST', `/bookings/${id}/confirm`, { auth: true }),
  completeBooking: (id) => request('POST', `/bookings/${id}/complete`, { auth: true }),
  listBlocks: (propertyId) => request('GET', `/properties/${propertyId}/blocks`, { auth: true }),
  createBlock: (propertyId, body) => request('POST', `/properties/${propertyId}/blocks`, { body, auth: true }),
  deleteBlock: (blockId) => request('DELETE', `/blocks/${blockId}`, { auth: true }),
  importCalendar: (propertyId, ical) => request('POST', `/properties/${propertyId}/calendar/import`, { body: { ical }, auth: true }),
  calendarFeedUrl: (propertyId) => `${BASE_URL}/properties/${propertyId}/calendar.ics`,

  // Availability (public)
  availability: (id, params = {}) => {
    const qs = new URLSearchParams(params).toString();
    return request('GET', `/properties/${id}/availability${qs ? `?${qs}` : ''}`);
  },

  // Messaging
  startConversation: (propertyId) => request('POST', '/conversations', { body: { propertyId }, auth: true }),
  listConversations: () => request('GET', '/conversations', { auth: true }),
  listMessages: (id) => request('GET', `/conversations/${id}/messages`, { auth: true }),
  sendMessage: (id, body) => request('POST', `/conversations/${id}/messages`, { body: { body }, auth: true }),
  markConversationRead: (id) => request('POST', `/conversations/${id}/read`, { auth: true }),
  messagesUnreadCount: () => request('GET', '/conversations/unread-count', { auth: true }),

  // Favorites
  listFavorites: () => request('GET', '/favorites', { auth: true }),
  addFavorite: (propertyId) => request('POST', '/favorites', { body: { propertyId }, auth: true }),
  removeFavorite: (propertyId) => request('DELETE', `/favorites/${propertyId}`, { auth: true }),

  // Notifications
  listNotifications: () => request('GET', '/notifications', { auth: true }),
  markNotificationRead: (id) => request('POST', `/notifications/${id}/read`, { auth: true }),
  markAllNotificationsRead: () => request('POST', '/notifications/read-all', { auth: true }),

  // Payments
  listPayments: () => request('GET', '/payments/me', { auth: true }),
  downloadReceipt: (bookingId) => downloadFile(`/bookings/${bookingId}/receipt`, `airhost-receipt-${bookingId}.pdf`),
};
