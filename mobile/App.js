import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { StatusBar } from 'expo-status-bar';
import { AuthProvider } from './src/auth/AuthContext';
import { RealtimeProvider } from './src/api/RealtimeContext';
import { useRegisterPushToken } from './src/notifications/pushBootstrap';
import { I18nProvider, useT } from './src/i18n/I18nContext';
import ExploreScreen from './src/screens/ExploreScreen';
import PropertyScreen from './src/screens/PropertyScreen';
import TripsScreen from './src/screens/TripsScreen';
import AccountScreen from './src/screens/AccountScreen';
import FavoritesScreen from './src/screens/FavoritesScreen';
import NotificationsScreen from './src/screens/NotificationsScreen';
import MessagesScreen from './src/screens/MessagesScreen';
import ConversationScreen from './src/screens/ConversationScreen';
import VerificationScreen from './src/screens/VerificationScreen';
import HostListingsScreen from './src/screens/HostListingsScreen';
import HostEarningsScreen from './src/screens/HostEarningsScreen';
import HostPropertyBookingsScreen from './src/screens/HostPropertyBookingsScreen';
import HostCalendarScreen from './src/screens/HostCalendarScreen';
import HostListingFormScreen from './src/screens/HostListingFormScreen';
import SplitsScreen from './src/screens/SplitsScreen';
import DisputeScreen from './src/screens/DisputeScreen';
import MyDisputesScreen from './src/screens/MyDisputesScreen';
import SavedRepliesScreen from './src/screens/SavedRepliesScreen';
import ExperiencesScreen from './src/screens/ExperiencesScreen';
import ExperienceDetailScreen from './src/screens/ExperienceDetailScreen';
import MyExperienceBookingsScreen from './src/screens/MyExperienceBookingsScreen';
import { HeaderAuthButton } from './src/screens/HeaderAuthButton';

const Stack = createNativeStackNavigator();

// PushBootstrap is a hooks-only component that registers the device with the
// backend on login and unregisters it on logout. Mounted inside AuthProvider
// (so it can read auth state) and RealtimeProvider (no dependency, just for
// consistency with the existing wiring).
function PushBootstrap() {
  useRegisterPushToken();
  return null;
}

// AppNavigator is split out so it can subscribe to the I18nContext via useT()
// and re-render the Stack.Screen options when the user picks a new locale —
// react-navigation re-reads the `title` from each screen's options on every
// render of the navigator, so the header label flips alongside the rest of
// the app. This mirrors how the web's <NavBar> consumes the same context.
// Screens added after S91's base (Splits/Dispute/Experiences/etc.) still
// use hardcoded English titles; a follow-up sweep wires them through t().
function AppNavigator() {
  const { t } = useT();
  return (
    <NavigationContainer>
      <Stack.Navigator
        screenOptions={{
          headerStyle: { backgroundColor: '#fff' },
          headerTitleStyle: { color: '#ff385c', fontWeight: '800' },
          headerRight: () => <HeaderAuthButton />,
        }}
      >
        <Stack.Screen name="Explore" component={ExploreScreen} options={{ title: 'AirHost' }} />
        <Stack.Screen name="Property" component={PropertyScreen} options={{ title: t('admin.listing') /* "Listing" / "Anúncio" / "Alojamiento" */ }} />
        <Stack.Screen name="Account" component={AccountScreen} options={{ title: t('nav.account') }} />
        <Stack.Screen name="Trips" component={TripsScreen} options={{ title: t('nav.trips') }} />
        <Stack.Screen name="Saved" component={FavoritesScreen} options={{ title: t('fav.title') }} />
        <Stack.Screen name="Notifications" component={NotificationsScreen} options={{ title: t('nav.notifications') }} />
        <Stack.Screen name="Verification" component={VerificationScreen} options={{ title: t('verify.title') }} />
        <Stack.Screen name="Messages" component={MessagesScreen} options={{ title: t('msg.title') }} />
        <Stack.Screen name="Conversation" component={ConversationScreen} options={{ title: t('msg.title') }} />
        <Stack.Screen name="HostListings" component={HostListingsScreen} options={{ title: t('nav.host') }} />
        <Stack.Screen name="HostEarnings" component={HostEarningsScreen} options={{ title: t('earnings.title') }} />
        <Stack.Screen name="HostPropertyBookings" component={HostPropertyBookingsScreen} options={{ title: t('host.bookings') }} />
        <Stack.Screen name="HostCalendar" component={HostCalendarScreen} options={{ title: t('ical.title') }} />
        <Stack.Screen name="HostListingForm" component={HostListingFormScreen} options={{ title: t('admin.listing') }} />
        {/* TODO(i18n): the screens below were added after the S91 base; titles still hardcoded. */}
        <Stack.Screen name="Splits" component={SplitsScreen} options={{ title: 'Split payments' }} />
        <Stack.Screen name="Dispute" component={DisputeScreen} options={{ title: 'Resolution case' }} />
        <Stack.Screen name="MyDisputes" component={MyDisputesScreen} options={{ title: 'Resolution cases' }} />
        <Stack.Screen name="SavedReplies" component={SavedRepliesScreen} options={{ title: 'Saved replies' }} />
        <Stack.Screen name="Experiences" component={ExperiencesScreen} options={{ title: 'Experiences' }} />
        <Stack.Screen name="ExperienceDetail" component={ExperienceDetailScreen} options={{ title: 'Experience' }} />
        <Stack.Screen name="MyExperienceBookings" component={MyExperienceBookingsScreen} options={{ title: 'My experiences' }} />
      </Stack.Navigator>
    </NavigationContainer>
  );
}

export default function App() {
  return (
    <I18nProvider>
      <AuthProvider>
        <RealtimeProvider>
          <PushBootstrap />
          <StatusBar style="dark" />
          <AppNavigator />
        </RealtimeProvider>
      </AuthProvider>
    </I18nProvider>
  );
}
