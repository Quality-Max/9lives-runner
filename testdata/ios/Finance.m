#import <UIKit/UIKit.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <UserNotifications/UserNotifications.h>
#ifndef FIXTURE_MODE
#define FIXTURE_MODE @"healthy"
#endif
// After sign-in: a field whose Return key does not close the keyboard, a
// button the keyboard covers at the bottom of the screen, and a notification
// permission request.
@interface Home : UIViewController <UITextFieldDelegate>
@property(nonatomic, strong) UILabel *status;
@end
@implementation Home
- (void)viewDidLoad {
  [super viewDidLoad]; self.view.backgroundColor = UIColor.systemBackgroundColor;
  UILabel *title = [[UILabel alloc] initWithFrame:CGRectMake(30,80,300,40)];
  title.accessibilityIdentifier = @"home.title"; title.text = @"Welcome"; [self.view addSubview:title];
  UITextField *note = [[UITextField alloc] initWithFrame:CGRectMake(30,140,300,50)];
  note.borderStyle = UITextBorderStyleRoundedRect; note.placeholder = @"Note";
  note.accessibilityIdentifier = @"note"; note.delegate = self; [self.view addSubview:note];
  UIButton *notify = [UIButton buttonWithType:UIButtonTypeSystem];
  notify.frame = CGRectMake(30,210,300,50); notify.accessibilityIdentifier = @"notify";
  [notify setTitle:@"Turn on notifications" forState:UIControlStateNormal];
  [notify addTarget:self action:@selector(notify:) forControlEvents:UIControlEventTouchUpInside];
  [self.view addSubview:notify];
  CGFloat bottom = UIScreen.mainScreen.bounds.size.height;
  UIButton *next = [UIButton buttonWithType:UIButtonTypeSystem];
  next.frame = CGRectMake(30,bottom-140,300,50); next.accessibilityIdentifier = @"continue";
  [next setTitle:@"Continue" forState:UIControlStateNormal];
  [next addTarget:self action:@selector(next:) forControlEvents:UIControlEventTouchUpInside];
  [self.view addSubview:next];
  self.status = [[UILabel alloc] initWithFrame:CGRectMake(30,280,300,50)];
  self.status.accessibilityIdentifier = @"home.status"; self.status.text = @"Ready"; [self.view addSubview:self.status];
}
// Return keeps the keyboard up, as many real forms do.
- (BOOL)textFieldShouldReturn:(UITextField *)field { self.status.text = [@"Noted " stringByAppendingString:field.text]; return NO; }
- (void)next:(id)sender { self.status.text = @"Continued"; }
- (void)notify:(id)sender {
  [UNUserNotificationCenter.currentNotificationCenter requestAuthorizationWithOptions:UNAuthorizationOptionAlert completionHandler:^(BOOL granted, NSError *error) {
    dispatch_async(dispatch_get_main_queue(), ^{ self.status.text = granted ? @"Notifications on" : @"Notifications off"; });
  }];
}
@end
@interface Finance : UIViewController <UITextFieldDelegate>
@property(nonatomic, strong) UITextField *amount;
@property(nonatomic, strong) UILabel *receipt;
@property(nonatomic, strong) UILabel *auth;
@property(nonatomic, strong) UITextField *username;
@property(nonatomic, strong) UITextField *password;
@end
@implementation Finance
- (void)viewDidLoad {
  [super viewDidLoad]; self.view.backgroundColor = UIColor.systemBackgroundColor;
  self.amount = [[UITextField alloc] initWithFrame:CGRectMake(30,100,300,50)];
  self.amount.borderStyle = UITextBorderStyleRoundedRect;
  self.amount.placeholder = @"Amount"; self.amount.accessibilityIdentifier = @"amount";
  [self.view addSubview:self.amount];
  for (int i=0; i<([FIXTURE_MODE isEqual:@"ambiguous"]?2:1); i++) {
    UIButton *send = [UIButton buttonWithType:UIButtonTypeSystem];
    send.frame = CGRectMake(30+i*160,180,140,50);
    [send setTitle:@"Send" forState:UIControlStateNormal];
    if (![FIXTURE_MODE isEqual:@"missing"] && ![FIXTURE_MODE isEqual:@"label-only"]) send.accessibilityIdentifier = @"send";
    if ([FIXTURE_MODE isEqual:@"label-only"]) send.accessibilityLabel = @"send";
    [send addTarget:self action:@selector(send:) forControlEvents:UIControlEventTouchUpInside];
    [self.view addSubview:send];
  }
  self.receipt = [[UILabel alloc] initWithFrame:CGRectMake(30,260,300,50)];
  self.receipt.accessibilityIdentifier = @"receipt"; self.receipt.text = @"Pending";
  [self.view addSubview:self.receipt];
  if ([FIXTURE_MODE isEqual:@"login"]) {
    // A password login with AutoFill content types, which is what makes iOS
    // offer to save the password once the login screen goes away.
    self.username = [[UITextField alloc] initWithFrame:CGRectMake(30,340,300,44)];
    self.username.borderStyle = UITextBorderStyleRoundedRect; self.username.placeholder = @"Email";
    self.username.textContentType = UITextContentTypeUsername; self.username.keyboardType = UIKeyboardTypeEmailAddress;
    self.username.autocapitalizationType = UITextAutocapitalizationTypeNone; self.username.accessibilityIdentifier = @"username";
    [self.view addSubview:self.username];
    self.password = [[UITextField alloc] initWithFrame:CGRectMake(30,394,300,44)];
    self.password.borderStyle = UITextBorderStyleRoundedRect; self.password.placeholder = @"Password";
    self.password.textContentType = UITextContentTypePassword; self.password.secureTextEntry = YES;
    self.password.returnKeyType = UIReturnKeyGo; self.password.delegate = self; self.password.accessibilityIdentifier = @"password";
    [self.view addSubview:self.password];
    UIButton *login = [UIButton buttonWithType:UIButtonTypeSystem];
    login.frame = CGRectMake(30,450,300,50); login.accessibilityIdentifier = @"log-in";
    [login setTitle:@"Log in" forState:UIControlStateNormal];
    [login addTarget:self action:@selector(logIn:) forControlEvents:UIControlEventTouchUpInside];
    [self.view addSubview:login];
  }
  if ([FIXTURE_MODE isEqual:@"biometric"]) {
    // A sign-in behind the system Face ID / Touch ID prompt. The status says
    // when the request starts, so a test can wait for it before presenting.
    UIButton *signIn = [UIButton buttonWithType:UIButtonTypeSystem];
    signIn.frame = CGRectMake(30,340,300,50); signIn.accessibilityIdentifier = @"sign-in";
    [signIn setTitle:@"Sign in with biometrics" forState:UIControlStateNormal];
    [signIn addTarget:self action:@selector(signIn:) forControlEvents:UIControlEventTouchUpInside];
    [self.view addSubview:signIn];
    self.auth = [[UILabel alloc] initWithFrame:CGRectMake(30,420,300,50)];
    self.auth.accessibilityIdentifier = @"auth.status"; self.auth.text = @"Locked";
    [self.view addSubview:self.auth];
  }
}
// Return in the password field logs in, as a real login form does.
- (BOOL)textFieldShouldReturn:(UITextField *)field { if (field == self.password) [self logIn:field]; return NO; }
- (void)logIn:(id)sender {
  if (self.username.text.length == 0 || self.password.text.length == 0) return;
  [self.view endEditing:YES];
  // The login screen leaves the window, as after a real sign-in; that is
  // when iOS offers to save the password.
  self.view.window.rootViewController = [Home new];
}
- (void)signIn:(id)sender {
  LAContext *context = [LAContext new]; NSError *error = nil;
  if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics error:&error]) { self.auth.text = @"Unavailable"; return; }
  self.auth.text = @"Prompting";
  [context evaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics localizedReason:@"Sign in" reply:^(BOOL success, NSError *failure) {
    dispatch_async(dispatch_get_main_queue(), ^{ self.auth.text = success ? @"Authenticated" : @"Not recognized"; });
  }];
}
- (void)send:(id)sender {
  NSString *documents = NSSearchPathForDirectoriesInDomains(NSDocumentDirectory,NSUserDomainMask,YES).firstObject;
  [@"started" writeToFile:[documents stringByAppendingPathComponent:@"started"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
  self.receipt.text = [FIXTURE_MODE isEqual:@"defect"] ? @"Sent $999" : [@"Sent $" stringByAppendingString:self.amount.text];
  [self.view endEditing:YES];
}
@end
@interface AppDelegate : UIResponder <UIApplicationDelegate>
@property(nonatomic, strong) UIWindow *window;
@end
@implementation AppDelegate
- (BOOL)application:(UIApplication *)application didFinishLaunchingWithOptions:(NSDictionary *)options {
  self.window = [[UIWindow alloc] initWithFrame:UIScreen.mainScreen.bounds];
  self.window.rootViewController = [Finance new]; [self.window makeKeyAndVisible]; return YES;
}
@end
int main(int argc, char **argv) { @autoreleasepool { return UIApplicationMain(argc,argv,nil,NSStringFromClass(AppDelegate.class)); } }
