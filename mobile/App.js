import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { StatusBar } from 'expo-status-bar';
import { AuthProvider } from './src/auth/AuthContext';
import { RealtimeProvider } from './src/api/RealtimeContext';
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
import { HeaderAuthButton } from './src/screens/HeaderAuthButton';

const Stack = createNativeStackNavigator();

export default function App() {
  return (
    <AuthProvider>
      <RealtimeProvider>
      <StatusBar style="dark" />
      <NavigationContainer>
        <Stack.Navigator
          screenOptions={{
            headerStyle: { backgroundColor: '#fff' },
            headerTitleStyle: { color: '#ff385c', fontWeight: '800' },
            headerRight: () => <HeaderAuthButton />,
          }}
        >
          <Stack.Screen name="Explore" component={ExploreScreen} options={{ title: 'AirHost' }} />
          <Stack.Screen name="Property" component={PropertyScreen} options={{ title: 'Listing' }} />
          <Stack.Screen name="Account" component={AccountScreen} options={{ title: 'Account' }} />
          <Stack.Screen name="Trips" component={TripsScreen} options={{ title: 'My trips' }} />
          <Stack.Screen name="Saved" component={FavoritesScreen} options={{ title: 'Saved listings' }} />
          <Stack.Screen name="Notifications" component={NotificationsScreen} options={{ title: 'Notifications' }} />
          <Stack.Screen name="Verification" component={VerificationScreen} options={{ title: 'Identity verification' }} />
          <Stack.Screen name="Messages" component={MessagesScreen} options={{ title: 'Messages' }} />
          <Stack.Screen name="Conversation" component={ConversationScreen} options={{ title: 'Conversation' }} />
          <Stack.Screen name="HostListings" component={HostListingsScreen} options={{ title: 'Host dashboard' }} />
          <Stack.Screen name="HostEarnings" component={HostEarningsScreen} options={{ title: 'Earnings & payouts' }} />
          <Stack.Screen name="HostPropertyBookings" component={HostPropertyBookingsScreen} options={{ title: 'Bookings' }} />
        </Stack.Navigator>
      </NavigationContainer>
      </RealtimeProvider>
    </AuthProvider>
  );
}
