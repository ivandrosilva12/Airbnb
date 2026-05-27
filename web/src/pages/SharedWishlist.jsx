import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import { useT } from '../i18n/I18nContext';

// SharedWishlist renders a publicly shared wishlist collection (read-only) by
// its share token. No authentication required.
export default function SharedWishlist() {
  const { token } = useParams();
  const { t } = useT();
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.getSharedCollection(token).then(setData).catch((e) => setError(e.message));
  }, [token]);

  if (error) return <div className="container"><p className="error">{t('fav.sharedNotFound')}</p></div>;
  if (!data) return <div className="container"><p>{t('common.loading')}</p></div>;

  return (
    <div className="container">
      <h1>{data.name}</h1>
      <p className="card-meta">{t('fav.sharedSubtitle')}</p>
      {(data.items || []).length === 0 ? (
        <p>{t('fav.none')}</p>
      ) : (
        <div className="grid">
          {data.items.map((p) => <PropertyCard key={p.id} property={p} />)}
        </div>
      )}
    </div>
  );
}
