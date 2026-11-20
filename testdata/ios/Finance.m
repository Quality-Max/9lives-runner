#import <UIKit/UIKit.h>
#ifndef FIXTURE_MODE
#define FIXTURE_MODE @"healthy"
#endif
@interface Finance : UIViewController
@property(nonatomic, strong) UITextField *amount;
@property(nonatomic, strong) UILabel *receipt;
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
