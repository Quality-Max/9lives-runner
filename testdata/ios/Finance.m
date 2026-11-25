#import <UIKit/UIKit.h>
#import <LocalAuthentication/LocalAuthentication.h>
#ifndef FIXTURE_MODE
#define FIXTURE_MODE @"healthy"
#endif
@interface Finance : UIViewController
@property(nonatomic, strong) UITextField *amount;
@property(nonatomic, strong) UILabel *receipt;
@property(nonatomic, strong) UILabel *auth;
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
