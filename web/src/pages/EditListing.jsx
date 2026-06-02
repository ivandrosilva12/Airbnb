import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// EditListing edits the fields the backend allows changing after creation
// (PATCH /properties/:id): title, description, pricing, capacity, policies and
// instant book. Address, type, rooms, amenities and photos are managed
// elsewhere (photos have their own screen).
export default function EditListing() {
  const { t } = useT();
  const { id } = useParams();
  const navigate = useNavigate();
  const [form, setForm] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    // Pull from the host endpoint so the arrival fields (sensitive) come back.
    api
      .getHostProperty(id)
      .then((p) =>
        setForm({
          title: p.title,
          description: p.description || '',
          price: (p.pricePerNight.amountCents / 100).toString(),
          cleaningFee: ((p.cleaningFee?.amountCents || 0) / 100).toString(),
          currency: p.pricePerNight.currency,
          maxGuests: p.maxGuests,
          cancellationPolicy: p.cancellationPolicy || 'flexible',
          weeklyDiscount: Math.round((p.weeklyDiscountPct || 0) * 100).toString(),
          monthlyDiscount: Math.round((p.monthlyDiscountPct || 0) * 100).toString(),
          taxRate: (Math.round((p.taxRatePct || 0) * 1000) / 10).toString(),
          weekendPrice: ((p.weekendPriceCents || 0) / 100).toString(),
          instantBook: !!p.instantBook,
          minNights: p.minNights || 1,
          maxNights: p.maxNights || '',
          guestsIncluded: p.guestsIncluded || '',
          extraGuestFee: ((p.extraGuestFee?.amountCents || 0) / 100).toString(),
          securityDeposit: ((p.securityDeposit?.amountCents || 0) / 100).toString(),
          checkInMethod: p.arrival?.checkInMethod || '',
          arrivalInstructions: p.arrival?.arrivalInstructions || '',
          wifiSsid: p.arrival?.wifiSsid || '',
          wifiPassword: p.arrival?.wifiPassword || '',
        }),
      )
      .catch((e) => setError(e.message));
  }, [id]);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await api.updateProperty(id, {
        title: form.title,
        description: form.description,
        priceCents: Math.round(Number(form.price) * 100),
        cleaningFeeCents: Math.round(Number(form.cleaningFee || 0) * 100),
        currency: form.currency,
        maxGuests: Number(form.maxGuests),
        cancellationPolicy: form.cancellationPolicy,
        weeklyDiscountPct: Number(form.weeklyDiscount || 0) / 100,
        monthlyDiscountPct: Number(form.monthlyDiscount || 0) / 100,
        taxRatePct: Number(form.taxRate || 0) / 100,
        weekendPriceCents: Math.round(Number(form.weekendPrice || 0) * 100),
        instantBook: form.instantBook,
        minNights: Number(form.minNights) || 1,
        maxNights: Number(form.maxNights) || 0,
        guestsIncluded: Number(form.guestsIncluded) || 0,
        extraGuestFeeCents: Math.round(Number(form.extraGuestFee || 0) * 100),
        securityDepositCents: Math.round(Number(form.securityDeposit || 0) * 100),
        checkInMethod: form.checkInMethod,
        arrivalInstructions: form.arrivalInstructions,
        wifiSsid: form.wifiSsid,
        wifiPassword: form.wifiPassword,
      });
      navigate('/host');
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  if (error && !form) return <div className="container"><p className="error">{error}</p></div>;
  if (!form) return <div className="container"><p>{t('common.loading')}</p></div>;

  return (
    <div className="container">
      <h1>{t('edit.title')}</h1>
      {error && <p className="error">{error}</p>}
      <PriceRulesPanel propertyId={id} currency={form.currency} />
      <CohostsPanel propertyId={id} />
      <HouseRulesPanel propertyId={id} />

      <form className="form-grid" onSubmit={submit}>
        <label>{t('create.fTitle')}<input required value={form.title} onChange={set('title')} /></label>
        <label className="full">{t('create.fDescription')}<textarea value={form.description} onChange={set('description')} /></label>
        <label>{t('create.fPrice')}<input required type="number" min="0" step="0.01" value={form.price} onChange={set('price')} /></label>
        <label>{t('create.fCleaning')}<input type="number" min="0" step="0.01" value={form.cleaningFee} onChange={set('cleaningFee')} /></label>
        <label>{t('create.fCurrency')}<input required maxLength="3" value={form.currency} onChange={set('currency')} /></label>
        <label>{t('create.fMaxGuests')}<input type="number" min="1" value={form.maxGuests} onChange={set('maxGuests')} /></label>
        <label>{t('create.fPolicy')}
          <select value={form.cancellationPolicy} onChange={set('cancellationPolicy')}>
            <option value="flexible">{t('create.policyFlexible')}</option>
            <option value="moderate">{t('create.policyModerate')}</option>
            <option value="strict">{t('create.policyStrict')}</option>
          </select>
        </label>
        <label>{t('create.fWeeklyDiscount')}<input type="number" min="0" max="100" step="1" value={form.weeklyDiscount} onChange={set('weeklyDiscount')} /></label>
        <label>{t('create.fMonthlyDiscount')}<input type="number" min="0" max="100" step="1" value={form.monthlyDiscount} onChange={set('monthlyDiscount')} /></label>
        <label>{t('create.fTaxRate')}<input type="number" min="0" max="100" step="0.1" value={form.taxRate} onChange={set('taxRate')} /></label>
        <label>{t('create.fWeekendPrice')}<input type="number" min="0" step="0.01" value={form.weekendPrice} onChange={set('weekendPrice')} /></label>
        <label>{t('create.fMinNights')}<input type="number" min="1" value={form.minNights} onChange={set('minNights')} /></label>
        <label>{t('create.fMaxNights')}<input type="number" min="0" value={form.maxNights} onChange={set('maxNights')} /></label>
        <label>{t('create.fGuestsIncluded')}<input type="number" min="1" value={form.guestsIncluded} onChange={set('guestsIncluded')} /></label>
        <label>{t('create.fExtraGuestFee')}<input type="number" min="0" step="0.01" value={form.extraGuestFee} onChange={set('extraGuestFee')} /></label>
        <label>{t('create.fSecurityDeposit')}<input type="number" min="0" step="0.01" value={form.securityDeposit} onChange={set('securityDeposit')} /></label>
        <fieldset className="full arrival-fieldset">
          <legend>{t('arrival.legend')}</legend>
          <p className="muted">{t('arrival.hint')}</p>
          <div className="form-grid">
            <label>{t('arrival.fCheckInMethod')}
              <select value={form.checkInMethod} onChange={set('checkInMethod')}>
                <option value="">{t('arrival.methodUnset')}</option>
                <option value="self_lockbox">{t('arrival.methodSelfLockbox')}</option>
                <option value="smart_lock">{t('arrival.methodSmartLock')}</option>
                <option value="key_exchange">{t('arrival.methodKeyExchange')}</option>
                <option value="host_greeting">{t('arrival.methodHostGreeting')}</option>
              </select>
            </label>
            <label>{t('arrival.fWifiSsid')}<input value={form.wifiSsid} onChange={set('wifiSsid')} maxLength={100} /></label>
            <label>{t('arrival.fWifiPassword')}<input value={form.wifiPassword} onChange={set('wifiPassword')} maxLength={200} /></label>
            <label className="full">{t('arrival.fInstructions')}<textarea value={form.arrivalInstructions} onChange={set('arrivalInstructions')} maxLength={2000} rows={4} /></label>
          </div>
        </fieldset>
        <label className="full instant-book-toggle">
          <input
            type="checkbox"
            checked={form.instantBook}
            onChange={(e) => setForm({ ...form, instantBook: e.target.checked })}
          />
          <span>
            <strong>{t('create.fInstantBook')}</strong>
            <small>{t('create.fInstantBookHint')}</small>
          </span>
        </label>
        <div className="full">
          <button className="btn btn-primary" type="submit" disabled={saving}>
            {saving ? t('edit.saving') : t('edit.save')}
          </button>
        </div>
      </form>
    </div>
  );
}

// PriceRulesPanel lets the host attach, list and remove seasonal/per-date price
// overrides on a listing. The list is reloaded after every mutation so it
// reflects server state without bookkeeping in the parent component.
function PriceRulesPanel({ propertyId, currency }) {
  const { t } = useT();
  const [rules, setRules] = useState([]);
  const [start, setStart] = useState('');
  const [end, setEnd] = useState('');
  const [price, setPrice] = useState('');
  const [label, setLabel] = useState('');
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  const reload = useCallback(() => {
    api
      .listPriceRules(propertyId)
      .then((r) => setRules(r || []))
      .catch((e) => setError(e.message));
  }, [propertyId]);

  useEffect(() => {
    reload();
  }, [reload]);

  async function add(e) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await api.createPriceRule(propertyId, {
        startDate: start,
        endDate: end,
        priceCents: Math.round(Number(price) * 100),
        label,
      });
      setStart('');
      setEnd('');
      setPrice('');
      setLabel('');
      reload();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  async function remove(ruleId) {
    try {
      await api.deletePriceRule(propertyId, ruleId);
      reload();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <section className="price-rules-panel">
      <h2>{t('priceRules.title')}</h2>
      <p className="muted">{t('priceRules.hint')}</p>
      {error && <p className="error">{error}</p>}
      <form className="form-grid price-rules-form" onSubmit={add}>
        <label>{t('priceRules.from')}<input required type="date" value={start} onChange={(e) => setStart(e.target.value)} /></label>
        <label>{t('priceRules.to')}<input required type="date" value={end} onChange={(e) => setEnd(e.target.value)} /></label>
        <label>{t('priceRules.price')} ({currency})<input required type="number" min="0" step="0.01" value={price} onChange={(e) => setPrice(e.target.value)} /></label>
        <label>{t('priceRules.label')}<input value={label} onChange={(e) => setLabel(e.target.value)} maxLength={60} /></label>
        <div className="full">
          <button type="submit" className="btn" disabled={saving}>
            {saving ? t('priceRules.adding') : t('priceRules.add')}
          </button>
        </div>
      </form>
      {rules.length === 0 ? (
        <p className="muted">{t('priceRules.empty')}</p>
      ) : (
        <ul className="price-rules-list">
          {rules.map((r) => (
            <li key={r.id}>
              <span>{r.startDate} → {r.endDate}</span>
              <strong>{(r.priceCents / 100).toFixed(2)} {r.currency}</strong>
              {r.label ? <em>{r.label}</em> : null}
              <button type="button" className="btn btn-link" onClick={() => remove(r.id)}>
                {t('priceRules.remove')}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// The four permissions a co-host can be granted on a single listing. Kept in
// sync with the backend's property.CohostPermission enum. The wire string is
// the canonical token; the label is i18n'd in CohostsPanel.
const COHOST_PERMS = ['manage_calendar', 'manage_pricing', 'reply_messages'];

// CohostsPanel lets the primary host invite, update and revoke per-listing
// co-hosts. The list is reloaded after every mutation so the UI reflects the
// server view without local optimistic bookkeeping.
function CohostsPanel({ propertyId }) {
  const { t } = useT();
  const [cohosts, setCohosts] = useState([]);
  const [email, setEmail] = useState('');
  const [perms, setPerms] = useState(new Set(['manage_calendar']));
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  const reload = useCallback(() => {
    api
      .listCohosts(propertyId)
      .then((r) => setCohosts(r.items || []))
      .catch((e) => setError(e.message));
  }, [propertyId]);

  useEffect(() => {
    reload();
  }, [reload]);

  function togglePerm(p) {
    const next = new Set(perms);
    if (next.has(p)) next.delete(p);
    else next.add(p);
    setPerms(next);
  }

  async function invite(e) {
    e.preventDefault();
    setError(null);
    if (perms.size === 0) {
      setError(t('cohosts.errNoPerms'));
      return;
    }
    setSaving(true);
    try {
      await api.inviteCohost(propertyId, {
        email: email.trim(),
        permissions: Array.from(perms),
      });
      setEmail('');
      setPerms(new Set(['manage_calendar']));
      reload();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  async function updatePerms(cohostId, nextSet) {
    setError(null);
    try {
      await api.updateCohostPermissions(propertyId, cohostId, Array.from(nextSet));
      reload();
    } catch (err) {
      setError(err.message);
    }
  }

  async function revoke(cohostId) {
    setError(null);
    try {
      await api.revokeCohost(propertyId, cohostId);
      reload();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <section className="cohosts-panel">
      <h2>{t('cohosts.title')}</h2>
      <p className="muted">{t('cohosts.hint')}</p>
      {error && <p className="error">{error}</p>}
      <form className="form-grid cohosts-form" onSubmit={invite}>
        <label className="full">
          {t('cohosts.fEmail')}
          <input
            required
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t('cohosts.fEmailPlaceholder')}
          />
        </label>
        <fieldset className="full cohosts-perms">
          <legend>{t('cohosts.permissions')}</legend>
          {COHOST_PERMS.map((p) => (
            <label key={p} className="cohosts-perm">
              <input
                type="checkbox"
                checked={perms.has(p)}
                onChange={() => togglePerm(p)}
              />
              <span>{t(`cohosts.perm.${p}`)}</span>
            </label>
          ))}
        </fieldset>
        <div className="full">
          <button type="submit" className="btn" disabled={saving}>
            {saving ? t('cohosts.inviting') : t('cohosts.invite')}
          </button>
        </div>
      </form>

      {cohosts.length === 0 ? (
        <p className="muted">{t('cohosts.empty')}</p>
      ) : (
        <ul className="cohosts-list">
          {cohosts.map((c) => (
            <li key={c.id} className="cohosts-list-item">
              <div>
                <strong>{c.displayName || c.email || c.userId}</strong>
                {c.email && c.displayName ? <small> ({c.email})</small> : null}
              </div>
              <div className="cohosts-perm-chips">
                {COHOST_PERMS.map((p) => {
                  const has = c.permissions.includes(p);
                  return (
                    <label key={p} className="cohosts-perm-chip">
                      <input
                        type="checkbox"
                        checked={has}
                        onChange={() => {
                          const next = new Set(c.permissions);
                          if (has) next.delete(p);
                          else next.add(p);
                          if (next.size === 0) {
                            setError(t('cohosts.errNoPerms'));
                            return;
                          }
                          updatePerms(c.id, next);
                        }}
                      />
                      <span>{t(`cohosts.perm.${p}`)}</span>
                    </label>
                  );
                })}
              </div>
              <button type="button" className="btn btn-link" onClick={() => revoke(c.id)}>
                {t('cohosts.revoke')}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// HouseRulesPanel lets the host author / edit the versioned rule set
// for a listing (S56). Every PATCH bumps the version on the server
// side; old versions stay queryable so historical acceptances still
// resolve to the exact text the guest saw. The component shows the
// CURRENT version below the items list so the host knows what guests
// are seeing right now and what version a future booking will
// acknowledge.
function HouseRulesPanel({ propertyId }) {
  const { t } = useT();
  const [items, setItems] = useState(['']);
  const [version, setVersion] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [savedMsg, setSavedMsg] = useState(null);

  // Initial load — re-fetch after a save returns so the rendered
  // version reflects the bump the server just did.
  const reload = useCallback(() => {
    api
      .getHouseRules(propertyId)
      .then((r) => {
        setVersion(r.version || 0);
        // Show at least one editable row so an empty rules set is
        // still authorable without an extra "add" click.
        setItems(r.items && r.items.length > 0 ? r.items : ['']);
      })
      .catch((e) => setError(e.message));
  }, [propertyId]);

  useEffect(() => {
    reload();
  }, [reload]);

  function updateItem(i, value) {
    setItems((prev) => prev.map((it, idx) => (idx === i ? value : it)));
    setSavedMsg(null);
  }
  function addItem() {
    setItems((prev) => [...prev, '']);
  }
  function removeItem(i) {
    setItems((prev) => prev.filter((_, idx) => idx !== i));
  }

  async function save(e) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSavedMsg(null);
    try {
      // Drop blanks so a user who hit "+" and didn't fill it in doesn't
      // get rejected by the server-side validation.
      const cleaned = items.map((s) => s.trim()).filter(Boolean);
      const r = await api.setHouseRules(propertyId, cleaned);
      setVersion(r.version);
      setItems(r.items.length > 0 ? r.items : ['']);
      setSavedMsg(t('houseRules.saved', { version: r.version }));
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="house-rules-panel">
      <h2>{t('houseRules.title')}</h2>
      <p className="muted">{t('houseRules.hint')}</p>
      {error && <p className="error">{error}</p>}
      <form onSubmit={save}>
        {items.map((it, i) => (
          <div key={i} className="rule-row">
            <input
              type="text"
              value={it}
              maxLength={500}
              placeholder={t('houseRules.placeholder')}
              aria-label={t('houseRules.itemAria', { n: i + 1 })}
              onChange={(e) => updateItem(i, e.target.value)}
            />
            {items.length > 1 && (
              <button type="button" className="btn btn-link" onClick={() => removeItem(i)} aria-label={t('houseRules.remove')}>
                ✕
              </button>
            )}
          </div>
        ))}
        <button type="button" className="btn btn-ghost" onClick={addItem}>
          {t('houseRules.add')}
        </button>
        <div className="rules-footer">
          <small className="muted">
            {version > 0 ? t('houseRules.currentVersion', { version }) : t('houseRules.notSet')}
          </small>
          <button className="btn btn-primary" type="submit" disabled={saving}>
            {saving ? t('houseRules.saving') : t('houseRules.save')}
          </button>
        </div>
        {savedMsg && <p className="success">{savedMsg}</p>}
      </form>
    </section>
  );
}
