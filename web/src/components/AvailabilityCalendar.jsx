// Renders the next `months` calendar grids, marking booked days. `booked` is an
// array of { checkIn, checkOut } (YYYY-MM-DD), interpreted as half-open ranges
// [checkIn, checkOut) — the check-out day is free for a new check-in.

function toKey(d) {
  return d.toISOString().slice(0, 10);
}

function buildBookedSet(booked) {
  const set = new Set();
  for (const r of booked) {
    const start = new Date(`${r.checkIn}T00:00:00Z`);
    const end = new Date(`${r.checkOut}T00:00:00Z`);
    for (let d = new Date(start); d < end; d.setUTCDate(d.getUTCDate() + 1)) {
      set.add(toKey(d));
    }
  }
  return set;
}

const WEEKDAYS = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'];

function Month({ year, month, bookedSet, todayKey }) {
  const first = new Date(Date.UTC(year, month, 1));
  const daysInMonth = new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
  // Monday-first offset (getUTCDay: 0=Sun).
  const offset = (first.getUTCDay() + 6) % 7;

  const cells = [];
  for (let i = 0; i < offset; i++) cells.push(<span key={`b${i}`} className="cal-cell cal-empty" />);
  for (let day = 1; day <= daysInMonth; day++) {
    const d = new Date(Date.UTC(year, month, day));
    const key = toKey(d);
    const booked = bookedSet.has(key);
    const past = key < todayKey;
    const cls = `cal-cell${booked ? ' cal-booked' : ''}${past ? ' cal-past' : ''}`;
    cells.push(<span key={key} className={cls}>{day}</span>);
  }

  const label = first.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  return (
    <div className="cal-month">
      <div className="cal-title">{label}</div>
      <div className="cal-grid">
        {WEEKDAYS.map((w) => (
          <span key={w} className="cal-weekday">{w}</span>
        ))}
        {cells}
      </div>
    </div>
  );
}

export default function AvailabilityCalendar({ booked = [], months = 2 }) {
  const bookedSet = buildBookedSet(booked);
  const todayKey = toKey(new Date());
  const now = new Date();
  const grids = [];
  for (let i = 0; i < months; i++) {
    const y = now.getUTCFullYear();
    const m = now.getUTCMonth() + i;
    grids.push(<Month key={i} year={y} month={m} bookedSet={bookedSet} todayKey={todayKey} />);
  }
  return (
    <div>
      <div className="cal-legend">
        <span><i className="dot dot-free" /> Available</span>
        <span><i className="dot dot-booked" /> Booked</span>
      </div>
      <div className="cal-wrap">{grids}</div>
    </div>
  );
}
